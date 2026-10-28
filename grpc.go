package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Wire-level Protobuf Codec: encodes raw TCP bytes into standard Protobuf Field 1 (bytes)
// Wire format: 0x0a (tag 1, wire type 2: length-delimited) + varint(len) + raw_bytes
type protoRawCodec struct{}

func (protoRawCodec) Name() string { return "proto" }

func (protoRawCodec) Marshal(v any) ([]byte, error) {
	data, ok := v.([]byte)
	if !ok {
		if ptr, isPtr := v.(*[]byte); isPtr {
			data = *ptr
		} else {
			return nil, fmt.Errorf("unexpected payload type: %T", v)
		}
	}
	var buf [10]byte
	n := binary.PutUvarint(buf[:], uint64(len(data)))
	header := append([]byte{0x0a}, buf[:n]...)
	return append(header, data...), nil
}

func (protoRawCodec) Unmarshal(data []byte, v any) error {
	ptr, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("unexpected target type: %T", v)
	}
	if len(data) == 0 {
		*ptr = (*ptr)[:0]
		return nil
	}
	// Parse Protobuf tag & length if present (field 1 wire type 2: 0x0a)
	if data[0] == 0x0a {
		length, n := binary.Uvarint(data[1:])
		if n > 0 && int(1+n+int(length)) <= len(data) {
			payload := data[1+n : 1+n+int(length)]
			*ptr = append((*ptr)[:0], payload...)
			return nil
		}
	}
	// Fallback to direct raw payload
	*ptr = append((*ptr)[:0], data...)
	return nil
}

func init() {
	encoding.RegisterCodec(protoRawCodec{})
}

// ---------------------------------------------------------
// EXIT NODE: gRPC Server Implementation
// ---------------------------------------------------------

type grpcTunnelServer struct{}

func startGRPCExitListener(t Tunnel) {
	listenAddr := "0.0.0.0:" + t.TunnelPort
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-EXIT-ERR [%s]: Failed to bind :%s: %v", t.Name, t.TunnelPort, err))
		return
	}

	mu.Lock()
	activeListeners[t.ID] = append(activeListeners[t.ID], lis)
	mu.Unlock()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{localCert},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS13,
	}

	s := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    15 * time.Second,
			Timeout: 5 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)

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

	addLog(fmt.Sprintf("GRPC-EXIT-OK [%s]: Listening on :%s (TLS 1.3 / ALPN h2)", t.Name, t.TunnelPort))
	go func() {
		if err := s.Serve(lis); err != nil {
			addLog(fmt.Sprintf("GRPC-EXIT-STOP [%s]: %v", t.Name, err))
		}
	}()
}

func handleGRPCTunnelStream(srv any, stream grpc.ServerStream) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	targetPort := "45454"
	if vals := md.Get("x-target-port"); len(vals) > 0 && vals[0] != "" {
		targetPort = vals[0]
	}

	targetConn, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 3*time.Second)
	if err != nil {
		return err
	}
	defer targetConn.Close()

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Uplink: gRPC Stream -> Local Target Port
	go func() {
		defer cancel()
		var inBuf []byte
		for {
			if err := stream.RecvMsg(&inBuf); err != nil {
				return
			}
			if len(inBuf) > 0 {
				if _, err := targetConn.Write(inBuf); err != nil {
					return
				}
			}
		}
	}()

	// Downlink: Local Target Port -> gRPC Stream
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			n, err := targetConn.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if err := stream.SendMsg(chunk); err != nil {
					return err
				}
			}
			if err != nil {
				return nil
			}
		}
	}
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
	grpcPoolsMu.Lock()
	pool, exists := grpcPools[remoteAddr]
	if !exists {
		pool = &grpcClientPool{target: remoteAddr, sni: sni, pin: certPin}
		grpcPools[remoteAddr] = pool
	}
	grpcPoolsMu.Unlock()

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if pool.conn != nil {
		state := pool.conn.GetState().String()
		if state == "READY" || state == "IDLE" {
			return pool.conn, nil
		}
		pool.conn.Close()
		pool.conn = nil
	}

	tlsConfig := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2"},
		MinVersion:         tls.VersionTLS13,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if certPin == "" {
				return nil
			}
			return verifyFingerprintPin(rawCerts, certPin)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, remoteAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
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

	sni := t.SNI
	if sni == "" {
		sni = "www.cloudflare.com"
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

		addLog(fmt.Sprintf("GRPC-RELAY-OK [%s]: Forwarding :%s -> gRPC :%s (Multiplexed)", t.Name, pStr, t.TunnelPort))

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

	grpcConn, err := getOrCreateGRPCConn(t.RemoteAddr, sni, t.CertPin)
	if err != nil {
		addLog(fmt.Sprintf("GRPC-DIAL-ERR [%s]: Failed to connect to Exit %s: %v", t.Name, t.RemoteAddr, err))
		return
	}

	// Attach target port header to stream context metadata
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-target-port", targetPort)

	// Open a new multiplexed bidirectional stream on the single HTTP/2 connection
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

	limiter := getTunnelLimiter(t.ID, t.SpeedLimit)

	// Uplink: User TCP Connection -> gRPC Stream
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := userConn.Read(buf)
			if n > 0 {
				if limiter != nil {
					_ = limiter.WaitN(ctx, n)
				}
				recordAppTraffic(uint64(n), 0)
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if err := stream.SendMsg(chunk); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Downlink: gRPC Stream -> User TCP Connection
	var inBuf []byte
	for {
		if err := stream.RecvMsg(&inBuf); err != nil {
			return
		}
		if len(inBuf) > 0 {
			recordAppTraffic(0, uint64(len(inBuf)))
			if _, err := userConn.Write(inBuf); err != nil {
				return
			}
		}
	}
}


func startGRPCEngine(t Tunnel) {
	if t.Role == "server" {
		startGRPCExitListener(t)
	} else {
		startGRPCEntryListener(t)
	}
}
