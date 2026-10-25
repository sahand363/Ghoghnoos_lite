package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"encoding/base64"
	"encoding/json"
)

type tcpSegmentConn struct {
	net.Conn
	fragSize   int
	packetsOut int
}

func (f *tcpSegmentConn) Write(b []byte) (int, error) {
	f.packetsOut++
	if f.fragSize <= 0 || f.packetsOut > 2 || len(b) <= f.fragSize {
		return f.Conn.Write(b)
	}
	total := 0
	for len(b) > 0 {
		chunk := f.fragSize
		if chunk > len(b) {
			chunk = len(b)
		}
		n, err := f.Conn.Write(b[:chunk])
		total += n
		if err != nil {
			return total, err
		}
		b = b[chunk:]
		time.Sleep(1 * time.Millisecond)
	}
	return total, nil
}

func buildTunnelTLSConfig(t Tunnel) *tls.Config {
	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))
	sniHost := strings.TrimSpace(t.SNI)
	if sniHost == "" {
		sniHost = "cloudflare-dns.com"
	}

	cfg := &tls.Config{
		ServerName:         sniHost,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}

	switch strings.ToLower(t.Fingerprint) {
	case "firefox":
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	case "safari":
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	default:
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	}

	if pinClean != "" {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate presented")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pinClean {
				addLog(fmt.Sprintf("SEC-ALERT [%s]: SHA-256 Certificate Pin mismatch!", t.Name))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}
	return cfg
}

func startDirectTLSEngine(t Tunnel) {
	mu.Lock()
	stopTunnelIfRunning(t.ID)
	mu.Unlock()

	inboundPorts := parsePortList(t.InboundPorts)
	if len(inboundPorts) == 0 {
		inboundPorts = []string{"2053"}
	}
	tunPort := strings.TrimSpace(t.TunnelPort)
	if tunPort == "" {
		tunPort = "10201"
	}

	if t.Role == "server" {
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{localCert}, MinVersion: tls.VersionTLS12}
		ln, err := tls.Listen("tcp", ":"+tunPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [DirectTLS-Exit %s]: Cannot bind Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [DirectTLS-Exit %s]: :%s -> X-UI 127.0.0.1:[%s]", t.Name, tunPort, strings.Join(inboundPorts, ",")))

		go func(l net.Listener, defPort string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go handleDirectTLSServerConn(conn, t, defPort)
			}
		}(ln, inboundPorts[0])
		return
	}

	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [DirectTLS-Entry %s]: Cannot bind Inbound :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [DirectTLS-Entry %s]: :%s -> %s (SNI: %s)", t.Name, iPort, t.RemoteAddr, t.SNI))

		go func(l net.Listener, targetPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handleDirectTLSClientConn(client, t, targetPort)
			}
		}(ln, iPort)
	}
}

func handleDirectTLSClientConn(client net.Conn, t Tunnel, targetPort string) {
	defer client.Close()

	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	rawConn, err := dialer.Dial("tcp", t.RemoteAddr)
	if err != nil {
		addLog(fmt.Sprintf("ERR [DirectTLS %s]: Dial %s failed: %v", t.Name, t.RemoteAddr, err))
		return
	}
	defer rawConn.Close()
	enableKeepAlive(rawConn)

	fragSz, _ := strconv.Atoi(t.FragSize)
	var baseConn net.Conn = rawConn
	if fragSz > 0 {
		baseConn = &tcpSegmentConn{Conn: rawConn, fragSize: fragSz}
	}

	tlsConn := tls.Client(baseConn, buildTunnelTLSConfig(t))
	if err := tlsConn.Handshake(); err != nil {
		addLog(fmt.Sprintf("ERR [DirectTLS %s]: Handshake error: %v", t.Name, err))
		return
	}
	defer tlsConn.Close()

	header := fmt.Sprintf("LTUN|%s\n", targetPort)
	if _, err := tlsConn.Write([]byte(header)); err != nil {
		return
	}
	br := bufio.NewReader(tlsConn)
	reply, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(reply, "OK") {
		addLog(fmt.Sprintf("ERR [DirectTLS %s]: Rejected by Exit server: %s", t.Name, strings.TrimSpace(reply)))
		return
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(tlsConn, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, client); done <- struct{}{} }()
	go func() { io.Copy(rxW, tlsConn); done <- struct{}{} }()
	<-done
}

func handleDirectTLSServerConn(conn net.Conn, t Tunnel, defaultPort string) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	firstLine, err := br.ReadString('\n')
	if err != nil {
		return
	}

if strings.HasPrefix(firstLine, "ENROLL|") {
		rawB64 := strings.TrimSpace(strings.TrimPrefix(firstLine, "ENROLL|"))
		if decoded, err := base64.StdEncoding.DecodeString(rawB64); err == nil {
			var hb EnrollHeartbeat
			if json.Unmarshal(decoded, &hb) == nil {
				rep := processIncomingEnroll(hb)
				b, _ := json.Marshal(rep)
				conn.Write(append(b, '\n'))
			}
		}
		return
	}
	
	if strings.HasPrefix(firstLine, "LTUN|") {
		targetPort := strings.TrimSpace(strings.TrimPrefix(firstLine, "LTUN|"))
		if strings.HasPrefix(targetPort, "probe:") {
			probePort := strings.TrimPrefix(targetPort, "probe:")
			if c, err := net.DialTimeout("tcp", "127.0.0.1:"+probePort, 2*time.Second); err == nil {
				c.Close()
				conn.Write([]byte("OK\n"))
			} else {
				conn.Write([]byte("ERR:X-UI Port Refused\n"))
			}
			return
		}

		target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
		if err != nil {
			conn.Write([]byte("ERR\n"))
			return
		}
		defer target.Close()
		conn.Write([]byte("OK\n"))

		mbLimit := parseSpeedLimit(t.SpeedLimit)
		txW := newThrottledWriter(target, &appRxBytes, mbLimit)
		rxW := newThrottledWriter(conn, &appTxBytes, mbLimit)

		done := make(chan struct{}, 2)
		go func() { io.Copy(txW, conn); done <- struct{}{} }()
		go func() { io.Copy(rxW, target); done <- struct{}{} }()
		<-done
	}
}
