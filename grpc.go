package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	expectedPin = strings.TrimSpace(expectedPin)
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

// ---------------------------------------------------------
// EXIT NODE: gRPC Server Implementation
// ---------------------------------------------------------

type grpcTunnelServer struct{}

var activeGRPCServers = make(map[string]*grpc.Server)

func startGRPCExitListener(t Tunnel) {
	listenAddr := "0.0.0.0:" + t.TunnelPort
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-EXIT-ERR [%s]: Failed to bind :%s: %v", t.Name, t.TunnelPort, err))
		return
	}

	mu.Lock()
	if oldSrv, ok := activeGRPCServers[t.ID]; ok && oldSrv != nil {
		oldSrv.Stop()
	}
	activeListeners[t.ID] = append(activeListeners[t.ID], lis)
	mu.Unlock()

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

	mu.Lock()
	activeGRPCServers[t.ID] = s
	mu.Unlock()

	// Register Streaming RPC: /GhoghnoosService/TunnelStream
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "GhoghnoosService",
		HandlerType: (*grpcTunnelServer)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "TunnelStream",
				Handler:       handleGRPCTunnelStream,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}, &grpcTunnelServer{})

	addLog(fmt.Sprintf("GRPC-EXIT-OK [%s]: Listening on :%s (TLS / ALPN h2)", t.Name, t.TunnelPort))
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
	targetPort := "45454"
	if vals := md.Get("x-target-port"); len(vals) > 0 && vals[0] != "" {
		targetPort = vals[0]
	}

	// Handle 1-RTT Latency Probe from Panel Ping Button
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

	var wg sync.WaitGroup
	wg.Add(2)

	// Uplink: gRPC Stream -> Local X-UI Target Port
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

	// Downlink: Local X-UI Target Port -> gRPC Stream
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

// ---------------------------------------------------------
// RELAY NODE: gRPC Client Multiplexer
// ---------------------------------------------------------

type grpcClientPool struct {
	mu     sync.Mutex
	conn   *grpc.ClientConn
	target string
	sni    string
	pin    string
}

var grpcPools = make(map[string]*grpcClientPool)
var grpcPoolsMu sync.Mutex

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

func startGRPCEntryListener(t Tunnel) {
	ports := parsePortList(t.InboundPorts)
	if len(ports) == 0 {
		return
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

		addLog(fmt.Sprintf("GRPC-RELAY-OK [%s]: Forwarding :%s -> gRPC %s (Multiplexed H2)", t.Name, pStr, t.RemoteAddr))

		go func() {
			for {
				conn, err := lis.Accept()
				if err != nil {
					return
				}
				go handleGRPCClientInbound(t, conn, pStr, sni)
			}
		}()
	}
}

func handleGRPCClientInbound(t Tunnel, userConn net.Conn, targetPort, sni string) {
	defer userConn.Close()

	// Always pick up latest CertPin for the target server
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

	// Uplink: User TCP Connection -> gRPC Stream
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

	// Downlink: gRPC Stream -> User TCP Connection
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
