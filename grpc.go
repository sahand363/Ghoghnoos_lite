package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func verifyGRPCCertPin(rawCerts [][]byte, expectedPin string) error {
	expectedPin = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(expectedPin, ":", "")))
	if expectedPin == "" {
		return nil
	}
	if len(rawCerts) == 0 {
		return fmt.Errorf("no peer certificate presented")
	}
	sum := sha256.Sum256(rawCerts[0])
	actualPin := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actualPin, expectedPin) {
		return fmt.Errorf("certificate pin mismatch: got %s, expected %s", actualPin, expectedPin)
	}
	return nil
}

type grpcTunnelService interface{}
type grpcTunnelServer struct{}

var activeGRPCServers = make(map[string]*grpc.Server)

func stopGRPCServerIfRunning(id string) {
	mu.Lock()
	srv, ok := activeGRPCServers[id]
	if ok {
		delete(activeGRPCServers, id)
	}
	mu.Unlock()
	if ok && srv != nil {
		srv.Stop()
	}
}

func newConfiguredGRPCServer() *grpc.Server {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{localCert},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS12,
	}
	s := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.InitialWindowSize(4*1024*1024),
		grpc.InitialConnWindowSize(16*1024*1024),
		grpc.MaxRecvMsgSize(16*1024*1024),
		grpc.MaxSendMsgSize(16*1024*1024),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    15 * time.Second,
			Timeout: 5 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "GhoghnoosService",
		HandlerType: (*grpcTunnelService)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "TunnelStream",
				Handler:       handleGRPCTunnelStream,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}, &grpcTunnelServer{})
	return s
}

func startGRPCControlListener(grpcPort string) {
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		addLog(fmt.Sprintf("WARN [Control-gRPC]: Could not bind :%s -> %v", grpcPort, err))
		return
	}
	s := newConfiguredGRPCServer()
	addLog(fmt.Sprintf("CONTROL-READY: gRPC HTTP/2 Ingress listening on :%s", grpcPort))
	if err := s.Serve(lis); err != nil {
		addLog(fmt.Sprintf("WARN [Control-gRPC]: Stopped :%s -> %v", grpcPort, err))
	}
}

func dialGRPCControl(targetAddr string, payloadBytes []byte) (EnrollReply, error) {
	var empty EnrollReply
	tlsConfig := &tls.Config{
		ServerName:         "www.speedtest.net",
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2"},
		MinVersion:         tls.VersionTLS12,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, targetAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithBlock(),
	)
	if err != nil {
		return empty, err
	}
	defer conn.Close()

	b64 := base64.StdEncoding.EncodeToString(payloadBytes)
	streamCtx := metadata.AppendToOutgoingContext(ctx, "x-enroll-payload", b64)
	streamDesc := &grpc.StreamDesc{
		StreamName:    "TunnelStream",
		ServerStreams: true,
		ClientStreams: true,
	}
	stream, err := conn.NewStream(streamCtx, streamDesc, "/GhoghnoosService/TunnelStream")
	if err != nil {
		return empty, err
	}
	var outMsg wrapperspb.BytesValue
	if err := stream.RecvMsg(&outMsg); err != nil {
		return empty, err
	}
	var rep EnrollReply
	if err := json.Unmarshal(outMsg.Value, &rep); err != nil || !rep.OK {
		return empty, fmt.Errorf("grpc enroll rejected")
	}
	return rep, nil
}

func startGRPCExitListener(t Tunnel) {
	tunPort := strings.TrimSpace(t.TunnelPort)
	if tunPort == "" {
		tunPort = "10201"
	}
	listenAddr := "0.0.0.0:" + tunPort
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-EXIT-ERR [%s]: Failed to bind :%s: %v", t.Name, tunPort, err))
		return
	}

	s := newConfiguredGRPCServer()
	mu.Lock()
	activeListeners[t.ID] = append(activeListeners[t.ID], lis)
	activeGRPCServers[t.ID] = s
	mu.Unlock()

	addLog(fmt.Sprintf("LISTEN [gRPC-Exit %s]: :%s (TLS / ALPN h2)", t.Name, tunPort))
	go func() {
		if err := s.Serve(lis); err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				addLog(fmt.Sprintf("GRPC-EXIT-STOP [%s]: %v", t.Name, err))
			}
		}
	}()
}

func handleGRPCTunnelStream(srv any, stream grpc.ServerStream) error {
	md, _ := metadata.FromIncomingContext(stream.Context())

	if vals := md.Get("x-enroll-payload"); len(vals) > 0 && vals[0] != "" {
		if !isControlMethodAllowed("grpc") {
			return fmt.Errorf("grpc control disabled")
		}
		if decoded, err := base64.StdEncoding.DecodeString(vals[0]); err == nil {
			var hb EnrollHeartbeat
			if json.Unmarshal(decoded, &hb) == nil {
				hb.ViaMethod = "GRPC"
				rep := processIncomingEnroll(hb)
				b, _ := json.Marshal(rep)
				_ = stream.SendMsg(&wrapperspb.BytesValue{Value: b})
				return nil
			}
		}
		return fmt.Errorf("invalid enroll payload")
	}

	targetPort := "2053"
	if vals := md.Get("x-target-port"); len(vals) > 0 && vals[0] != "" {
		targetPort = vals[0]
	}

	if strings.HasPrefix(targetPort, "probe:") {
		var inMsg wrapperspb.BytesValue
		_ = stream.RecvMsg(&inMsg)
		_ = stream.SendMsg(&wrapperspb.BytesValue{Value: []byte("OK")})
		return nil
	}

	targetConn, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 4*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-EXIT-DIAL-ERR: Local port 127.0.0.1:%s unreachable: %v", targetPort, err))
		return err
	}
	defer targetConn.Close()
	enableKeepAlive(targetConn)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			var inMsg wrapperspb.BytesValue
			if err := stream.RecvMsg(&inMsg); err != nil {
				if tcpConn, ok := targetConn.(*net.TCPConn); ok {
					_ = tcpConn.CloseWrite()
				}
				return
			}
			if len(inMsg.Value) > 0 {
				atomic.AddUint64(&appRxBytes, uint64(len(inMsg.Value)))
				if _, err := targetConn.Write(inMsg.Value); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if n > 0 {
				atomic.AddUint64(&appTxBytes, uint64(n))
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				outMsg := &wrapperspb.BytesValue{Value: chunk}
				if sendErr := stream.SendMsg(outMsg); sendErr != nil {
					targetConn.Close()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

type grpcClientPool struct {
	mu     sync.Mutex
	conn   *grpc.ClientConn
	target string
	sni    string
	pin    string
}

var (
	grpcPools   = make(map[string]*grpcClientPool)
	grpcPoolsMu sync.Mutex
)

func getOrCreateGRPCConn(remoteAddr, sni, certPin string) (*grpc.ClientConn, error) {
	poolKey := fmt.Sprintf("%s|%s|%s", remoteAddr, sni, certPin)
	grpcPoolsMu.Lock()
	pool, exists := grpcPools[poolKey]
	if !exists {
		pool = &grpcClientPool{target: remoteAddr, sni: sni, pin: certPin}
		grpcPools[poolKey] = pool
	}
	grpcPoolsMu.Unlock()

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if pool.conn != nil {
		st := pool.conn.GetState()
		if st == connectivity.Ready || st == connectivity.Idle || st == connectivity.Connecting {
			return pool.conn, nil
		}
		_ = pool.conn.Close()
		pool.conn = nil
	}

	tlsConfig := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2"},
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyGRPCCertPin(rawCerts, certPin)
		},
	}

	conn, err := grpc.Dial(remoteAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithInitialWindowSize(4*1024*1024),
		grpc.WithInitialConnWindowSize(16*1024*1024),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(16*1024*1024),
			grpc.MaxCallSendMsgSize(16*1024*1024),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                15 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, err
	}

	pool.conn = conn
	return conn, nil
}

func probeGRPCTunnelOnce(tun Tunnel, firstPort string, pt *PingPoint) {
	sni := strings.TrimSpace(tun.SNI)
	if sni == "" {
		sni = "www.speedtest.net"
	}
	certPin := tun.CertPin
	mu.Lock()
	if dstSrv := findServerByID(tun.TargetServer); dstSrv != nil && dstSrv.CertPin != "" {
		certPin = dstSrv.CertPin
	}
	mu.Unlock()

	grpcConn, err := getOrCreateGRPCConn(tun.RemoteAddr, sni, certPin)
	if err != nil {
		pt.OK = false
		pt.Error = "gRPC Dial Failed"
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-target-port", "probe:"+firstPort)

	streamDesc := &grpc.StreamDesc{
		StreamName:    "TunnelStream",
		ServerStreams: true,
		ClientStreams: true,
	}

	rttStart := time.Now()
	stream, err := grpcConn.NewStream(ctx, streamDesc, "/GhoghnoosService/TunnelStream")
	if err != nil {
		pt.OK = false
		pt.Error = "gRPC Stream Error"
		return
	}
	_ = stream.SendMsg(&wrapperspb.BytesValue{Value: []byte("PING")})
	var outMsg wrapperspb.BytesValue
	err = stream.RecvMsg(&outMsg)
	pt.LatencyMS = time.Since(rttStart).Milliseconds()
	pt.OK = err == nil && strings.HasPrefix(string(outMsg.Value), "OK")
	if !pt.OK {
		pt.Error = "gRPC Probe Rejected"
	}
}

func startGRPCEntryListener(t Tunnel) {
	ports := parsePortList(t.InboundPorts)
	if len(ports) == 0 {
		ports = []string{"2053"}
	}

	sni := strings.TrimSpace(t.SNI)
	if sni == "" {
		sni = "www.speedtest.net"
	}

	for _, p := range ports {
		pStr := p
		lis, err := net.Listen("tcp", "0.0.0.0:"+pStr)
		if err != nil {
			addLog(fmt.Sprintf("GRPC-RELAY-ERR [%s]: Failed to bind inbound :%s: %v", t.Name, pStr, err))
			continue
		}

		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], lis)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [gRPC-Entry %s]: :%s -> %s (Multiplexed H2 / SNI: %s)", t.Name, pStr, t.RemoteAddr, sni))

		go func(l net.Listener, targetPort string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(conn)
				go handleGRPCClientInbound(t, conn, targetPort, sni)
			}
		}(lis, pStr)
	}
}

func handleGRPCClientInbound(t Tunnel, userConn net.Conn, targetPort, sni string) {
	defer userConn.Close()

	certPin := t.CertPin
	mu.Lock()
	if dstSrv := findServerByID(t.TargetServer); dstSrv != nil && dstSrv.CertPin != "" {
		certPin = dstSrv.CertPin
	}
	mu.Unlock()

	grpcConn, err := getOrCreateGRPCConn(t.RemoteAddr, sni, certPin)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-DIAL-ERR [%s]: Failed to connect to Exit %s: %v", t.Name, t.RemoteAddr, err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-target-port", targetPort)

	streamDesc := &grpc.StreamDesc{
		StreamName:    "TunnelStream",
		ServerStreams: true,
		ClientStreams: true,
	}

	stream, err := grpcConn.NewStream(ctx, streamDesc, "/GhoghnoosService/TunnelStream")
	if err != nil {
		addLog(fmt.Sprintf("GRPC-STREAM-ERR [%s]: %v", t.Name, err))
		return
	}

	limiter := getTunnelLimiter(t.ID)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := userConn.Read(buf)
			if n > 0 {
				if limiter != nil {
					limiter.WaitBytes(n)
				}
				atomic.AddUint64(&appTxBytes, uint64(n))
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				outMsg := &wrapperspb.BytesValue{Value: chunk}
				if sendErr := stream.SendMsg(outMsg); sendErr != nil {
					return
				}
			}
			if err != nil {
				_ = stream.CloseSend()
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			var inMsg wrapperspb.BytesValue
			if err := stream.RecvMsg(&inMsg); err != nil {
				if tcpConn, ok := userConn.(*net.TCPConn); ok {
					_ = tcpConn.CloseWrite()
				}
				return
			}
			if len(inMsg.Value) > 0 {
				if limiter != nil {
					limiter.WaitBytes(len(inMsg.Value))
				}
				atomic.AddUint64(&appRxBytes, uint64(len(inMsg.Value)))
				if _, err := userConn.Write(inMsg.Value); err != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
}

func startGRPCEngine(t Tunnel) {
	stopTunnelIfRunning(t.ID)

	if t.Role == "server" {
		startGRPCExitListener(t)
	} else {
		startGRPCEntryListener(t)
	}
}
