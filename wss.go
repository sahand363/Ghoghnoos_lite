package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type byteCounterWriter struct {
	w       io.Writer
	counter *uint64
}

func (b *byteCounterWriter) Write(p []byte) (int, error) {
	n, err := b.w.Write(p)
	if n > 0 {
		atomic.AddUint64(b.counter, uint64(n))
	}
	return n, err
}

// Low-level packet fragmenter: splits outgoing TLS ClientHello records into small TCP segments to defeat DPI
type fragmentConn struct {
	net.Conn
	fragSize   int
	packetsOut int
}

func (f *fragmentConn) Write(b []byte) (int, error) {
	f.packetsOut++
	if f.fragSize <= 0 || f.packetsOut > 3 || len(b) <= f.fragSize {
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
		time.Sleep(2 * time.Millisecond)
	}
	return total, nil
}

// Crafts a real binary RFC-5246 TLS ClientHello packet carrying the decoy/whitelisted SNI
func buildFakeClientHello(fakeSNI string) []byte {
	if fakeSNI == "" {
		fakeSNI = "ebank.bmi.ir"
	}
	sniBytes := []byte(fakeSNI)
	var ext bytes.Buffer
	// Extension: server_name (0x0000)
	binary.Write(&ext, binary.BigEndian, uint16(0x0000))
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)+5))
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)+3))
	ext.WriteByte(0x00)
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)))
	ext.Write(sniBytes)

	var ch bytes.Buffer
	ch.Write([]byte{0x03, 0x03}) // TLS 1.2 record version
	rnd := make([]byte, 32)
	rand.Read(rnd)
	ch.Write(rnd)
	ch.WriteByte(0x00)                                     // SessionID len 0
	ch.Write([]byte{0x00, 0x04, 0x13, 0x01, 0xcca8, 0x01}) // Ciphers + Compression
	binary.Write(&ch, binary.BigEndian, uint16(ext.Len()))
	ch.Write(ext.Bytes())

	var handshake bytes.Buffer
	handshake.WriteByte(0x01) // ClientHello type
	l := ch.Len()
	handshake.Write([]byte{byte(l >> 16), byte(l >> 8), byte(l)})
	handshake.Write(ch.Bytes())

	var record bytes.Buffer
	record.Write([]byte("SNISPOOF")) // 8-byte sync magic for Exit Sentinel
	binary.Write(&record, binary.BigEndian, uint16(handshake.Len()))
	record.Write(handshake.Bytes())
	return record.Bytes()
}

func buildSpoofTLSConfig(t Tunnel) *tls.Config {
	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))
	cfg := &tls.Config{
		ServerName:         t.SNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	switch strings.ToLower(t.Fingerprint) {
	case "firefox":
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	case "safari":
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	default: // Chrome 120
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	}
	if pinClean != "" {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pinClean {
				addLog(fmt.Sprintf("SEC-ALERT [%s]: Cert Pin mismatch!", t.Name))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}
	return cfg
}

func computeWSAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func enableKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(15 * time.Second)
		_ = tcp.SetNoDelay(true)
	}
}

func parsePortList(raw string) []string {
	var res []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if strings.Contains(p, ":") {
			segs := strings.Split(p, ":")
			p = segs[len(segs)-1]
		}
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

func stopTunnelIfRunning(id string) {
	if list, ok := activeListeners[id]; ok {
		for _, ln := range list {
			_ = ln.Close()
		}
		delete(activeListeners, id)
	}
}

func startTunnelEngine(t Tunnel) {
	mu.Lock()
	stopTunnelIfRunning(t.ID)
	mu.Unlock()

	if t.Method == "" {
		t.Method = "wss"
	}
	t.WSPath = strings.TrimSpace(t.WSPath)
	if t.WSPath == "" {
		t.WSPath = "/ws"
	}
	if !strings.HasPrefix(t.WSPath, "/") {
		t.WSPath = "/" + t.WSPath
	}
	if t.SNI == "" {
		t.SNI = "www.speedtest.net"
	}

	inboundPorts := parsePortList(t.InboundPorts)
	if len(inboundPorts) == 0 {
		inboundPorts = []string{"2053"}
	}
	tunPort := strings.TrimSpace(t.TunnelPort)
	if tunPort == "" {
		tunPort = "10201"
	}

	if t.Role == "server" {
		ln, err := net.Listen("tcp", ":"+tunPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Exit %s]: Cannot bind Tunnel Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [%s-Exit %s]: :%s -> X-UI 127.0.0.1:[%s]", strings.ToUpper(t.Method), t.Name, tunPort, strings.Join(inboundPorts, ",")))

		go func(l net.Listener, defPort string) {
			for {
				rawConn, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(rawConn)
				go handleServerConn(rawConn, t, defPort)
			}
		}(ln, inboundPorts[0])
		return
	}

	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Entry %s]: Cannot bind Inbound :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [%s-Entry %s]: :%s -> %s (FakeSNI: %s)", strings.ToUpper(t.Method), t.Name, iPort, t.RemoteAddr, t.FakeSNI))

		go func(l net.Listener, targetPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handleClientConn(client, t, targetPort)
			}
		}(ln, iPort)
	}
}

func dialTunnelSession(t Tunnel, targetPort string) (net.Conn, *bufio.Reader, error) {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	rawConn, err := dialer.Dial("tcp", t.RemoteAddr)
	if err != nil {
		return nil, nil, err
	}
	enableKeepAlive(rawConn)

	// METHOD 2: SNI SPOOFING + DECOY CLIENTHELLO INJECTION + RECORD FRAGMENTATION
	if t.Method == "spoof" {
		if t.FakeSNI != "" {
			_, _ = rawConn.Write(buildFakeClientHello(t.FakeSNI))
		}
		fragSz, _ := strconv.Atoi(t.FragSize)
		if fragSz <= 0 {
			fragSz = 64
		}
		fConn := &fragmentConn{Conn: rawConn, fragSize: fragSz}
		tlsConn := tls.Client(fConn, buildSpoofTLSConfig(t))
		if err := tlsConn.Handshake(); err != nil {
			rawConn.Close()
			return nil, nil, err
		}
		header := fmt.Sprintf("LTUN|%s\n", targetPort)
		if _, err := tlsConn.Write([]byte(header)); err != nil {
			tlsConn.Close()
			return nil, nil, err
		}
		br := bufio.NewReader(tlsConn)
		reply, err := br.ReadString('\n')
		if err != nil || !strings.HasPrefix(reply, "OK") {
			tlsConn.Close()
			return nil, nil, fmt.Errorf("spoof handshake rejected: %s", strings.TrimSpace(reply))
		}
		return tlsConn, br, nil
	}

	// METHOD 1: WSS (WebSocket over TLS 1.3)
	tlsConn := tls.Client(rawConn, buildSpoofTLSConfig(t))
	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, nil, err
	}
	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nX-Target-Port: %s\r\n\r\n",
		t.WSPath, t.SNI, wsKey, targetPort)
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		tlsConn.Close()
		return nil, nil, err
	}
	br := bufio.NewReader(tlsConn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		tlsConn.Close()
		return nil, nil, fmt.Errorf("WSS rejected: %s", strings.TrimSpace(statusLine))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
	}
	return tlsConn, br, nil
}

func handleClientConn(client net.Conn, t Tunnel, targetPort string) {
	defer client.Close()
	remoteTLS, br, err := dialTunnelSession(t, targetPort)
	if err != nil {
		addLog(fmt.Sprintf("ERR [%s]: %v", t.Name, err))
		return
	}
	defer remoteTLS.Close()

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		client.Write(buf)
	}

	done := make(chan struct{}, 2)
	go func() { io.Copy(&byteCounterWriter{w: remoteTLS, counter: &appTxBytes}, client); done <- struct{}{} }()
	go func() { io.Copy(&byteCounterWriter{w: client, counter: &appRxBytes}, remoteTLS); done <- struct{}{} }()
	<-done
}

func handleServerConn(rawConn net.Conn, t Tunnel, defaultPort string) {
	defer rawConn.Close()

	// Strip injected SNISPOOF decoy ClientHello packet if present before starting real TLS Server
	brRaw := bufio.NewReader(rawConn)
	peek, err := brRaw.Peek(8)
	if err == nil && string(peek) == "SNISPOOF" {
		_, _ = brRaw.Discard(8)
		var lenBuf [2]byte
		if _, err := io.ReadFull(brRaw, lenBuf[:]); err == nil {
			decoyLen := int(binary.BigEndian.Uint16(lenBuf[:]))
			_, _ = io.CopyN(io.Discard, brRaw, int64(decoyLen))
		}
	}

	tlsCfg := &tls.Config{Certificates: []tls.Certificate{localCert}, MinVersion: tls.VersionTLS12}
	conn := tls.Server(&bufferedConn{Conn: rawConn, r: brRaw}, tlsCfg)
	if err := conn.Handshake(); err != nil {
		return
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	firstLine, err := br.ReadString('\n')
	if err != nil {
		return
	}

	// Handle Method 2: Native SNI Spoof Stream ("LTUN|<port>\n")
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
		pipeStreams(conn, target, br)
		return
	}

	// Handle Method 1: WSS Stream
	parts := strings.Fields(firstLine)
	if len(parts) < 2 || parts[1] != t.WSPath {
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<html><body><h1>200 OK</h1></body></html>"))
		return
	}
	var wsKey string
	targetPort := defaultPort
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "sec-websocket-key:") {
			wsKey = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		} else if strings.HasPrefix(low, "x-target-port:") {
			targetPort = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	if strings.HasPrefix(targetPort, "probe:") {
		probePort := strings.TrimPrefix(targetPort, "probe:")
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+probePort, 2*time.Second); err == nil {
			c.Close()
			conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
		} else {
			conn.Write([]byte("HTTP/1.1 502 X-UI Port Refused\r\n\r\n"))
		}
		return
	}
	target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()
	conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
	pipeStreams(conn, target, br)
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func pipeStreams(conn net.Conn, target net.Conn, br *bufio.Reader) {
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}
	done := make(chan struct{}, 2)
	go func() { io.Copy(&byteCounterWriter{w: target, counter: &appRxBytes}, conn); done <- struct{}{} }()
	go func() { io.Copy(&byteCounterWriter{w: conn, counter: &appTxBytes}, target); done <- struct{}{} }()
	<-done
}

func probeTunnelOnce(tun Tunnel) PingPoint {
	ports := parsePortList(tun.InboundPorts)
	firstPort := "2053"
	if len(ports) > 0 {
		firstPort = ports[0]
	}
	pt := PingPoint{Timestamp: time.Now().Unix(), TunnelID: tun.ID, ServerID: tun.TargetServer}

	tlsCfg := buildSpoofTLSConfig(tun)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
	if err != nil {
		pt.OK = false
		pt.Error = "Tunnel Offline"
		recordPingPoint(pt)
		return pt
	}
	defer conn.Close()

	rttStart := time.Now()
	if tun.Method == "spoof" {
		conn.Write([]byte("LTUN|probe:" + firstPort + "\n"))
		reply, err := bufio.NewReader(conn).ReadString('\n')
		pt.LatencyMS = time.Since(rttStart).Milliseconds()
		pt.OK = err == nil && strings.HasPrefix(reply, "OK")
		if !pt.OK {
			pt.Error = "X-UI :" + firstPort + " Refused"
		}
		recordPingPoint(pt)
		return pt
	}

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nX-Target-Port: probe:%s\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		tun.WSPath, tun.SNI, firstPort, wsKey)
	conn.Write([]byte(req))
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	pt.LatencyMS = time.Since(rttStart).Milliseconds()
	pt.OK = err == nil && strings.Contains(statusLine, "101")
	if !pt.OK {
		pt.Error = "X-UI :" + firstPort + " Refused"
	}
	recordPingPoint(pt)
	return pt
}

func startPingMonitor() {
	for {
		time.Sleep(30 * time.Second)
		mu.Lock()
		tuns := append([]Tunnel{}, db.Tunnels...)
		mu.Unlock()
		for _, t := range tuns {
			if t.Role != "server" && t.RemoteAddr != "" {
				probeTunnelOnce(t)
			}
		}
	}
}
