package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type sharedThrottleBucket struct {
	mu               sync.Mutex
	bytesLimitPerSec int64
	bytesSentThisSec int64
	lastSecStart     time.Time
}

type throttledWriter struct {
	w       io.Writer
	counter *uint64
	bucket  *sharedThrottleBucket
}

var (
	sharedBuckets   = make(map[int64]*sharedThrottleBucket)
	sharedBucketsMu sync.Mutex
)

func newThrottledWriter(w io.Writer, counter *uint64, mbPerSec float64) io.Writer {
	limit := int64(mbPerSec * 1024.0 * 1024.0)
	if limit <= 0 {
		return &throttledWriter{w: w, counter: counter, bucket: nil}
	}
	sharedBucketsMu.Lock()
	b, ok := sharedBuckets[limit]
	if !ok {
		b = &sharedThrottleBucket{
			bytesLimitPerSec: limit,
			lastSecStart:     time.Now(),
		}
		sharedBuckets[limit] = b
	}
	sharedBucketsMu.Unlock()
	return &throttledWriter{
		w:       w,
		counter: counter,
		bucket:  b,
	}
}

func parseSpeedLimit(raw string) float64 {
	val, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || val < 0 {
		return 0
	}
	return val
}

func (t *throttledWriter) Write(p []byte) (int, error) {
	if t.bucket == nil || t.bucket.bytesLimitPerSec <= 0 {
		n, err := t.w.Write(p)
		if n > 0 {
			atomic.AddUint64(t.counter, uint64(n))
		}
		return n, err
	}
	total := 0
	for len(p) > 0 {
		t.bucket.mu.Lock()
		now := time.Now()
		if now.Sub(t.bucket.lastSecStart) >= 250*time.Millisecond {
			t.bucket.bytesSentThisSec = 0
			t.bucket.lastSecStart = now
		}
		windowLimit := t.bucket.bytesLimitPerSec / 4
		if windowLimit < 16384 {
			windowLimit = 16384
		}
		avail := windowLimit - t.bucket.bytesSentThisSec
		if avail <= 0 {
			t.bucket.mu.Unlock()
			time.Sleep(15 * time.Millisecond)
			continue
		}
		chunk := int64(len(p))
		if chunk > avail {
			chunk = avail
		}
		if chunk > 32768 {
			chunk = 32768
		}
		t.bucket.bytesSentThisSec += chunk
		t.bucket.mu.Unlock()

		n, err := t.w.Write(p[:chunk])
		total += n
		if n > 0 {
			atomic.AddUint64(t.counter, uint64(n))
		}
		if err != nil {
			return total, err
		}
		p = p[chunk:]
	}
	return total, nil
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
	mu.Lock()
	if list, ok := activeListeners[id]; ok {
		for _, ln := range list {
			_ = ln.Close()
		}
		delete(activeListeners, id)
	}
	mu.Unlock()
	stopGRPCServerIfRunning(id)
}

func startWSSEngine(t Tunnel) {
	stopTunnelIfRunning(t.ID)

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
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{localCert}, MinVersion: tls.VersionTLS12}
		ln, err := tls.Listen("tcp", ":"+tunPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Exit %s]: Cannot bind TLS Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [WSS-Exit %s]: :%s -> 127.0.0.1:[%s]", t.Name, tunPort, strings.Join(inboundPorts, ",")))

		go func(l net.Listener, defPort string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go handleWSSServerConn(conn, t, defPort)
			}
		}(ln, inboundPorts[0])
		return
	}

	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Entry %s]: Cannot bind Inbound :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [WSS-Entry %s]: :%s -> %s (SNI: %s)", t.Name, iPort, t.RemoteAddr, t.SNI))

		go func(l net.Listener, targetPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handleWSSClientConn(client, t, targetPort)
			}
		}(ln, iPort)
	}
}

func handleWSSClientConn(client net.Conn, t Tunnel, targetPort string) {
	defer client.Close()

	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	rawConn, err := dialer.Dial("tcp", t.RemoteAddr)
	if err != nil {
		addLog(fmt.Sprintf("ERR [WSS %s]: %v", t.Name, err))
		return
	}
	defer rawConn.Close()
	enableKeepAlive(rawConn)

	tlsConn := tls.Client(rawConn, buildTunnelTLSConfig(t))
	if err := tlsConn.Handshake(); err != nil {
		addLog(fmt.Sprintf("ERR [WSS %s]: TLS Handshake failed: %v", t.Name, err))
		return
	}
	defer tlsConn.Close()

	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nX-Target-Port: %s\r\n\r\n",
		t.WSPath, t.SNI, wsKey, targetPort)
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		return
	}

	br := bufio.NewReader(tlsConn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		addLog(fmt.Sprintf("ERR [WSS %s]: Upgrade rejected: %s", t.Name, strings.TrimSpace(statusLine)))
		return
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
	}

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		_, _ = br.Read(buf)
		_, _ = client.Write(buf)
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(tlsConn, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(txW, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(rxW, tlsConn); done <- struct{}{} }()
	<-done
}

func handleWSSServerConn(conn net.Conn, t Tunnel, defaultPort string) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	firstLine, err := br.ReadString('\n')
	if err != nil {
		return
	}

	parts := strings.Fields(firstLine)
	if len(parts) < 2 || parts[1] != t.WSPath {
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<html><body><h1>200 OK</h1></body></html>"))
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
		} else if strings.HasPrefix(low, "x-enroll-payload:") {
			rawB64 := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if decoded, err := base64.StdEncoding.DecodeString(rawB64); err == nil {
				var hb EnrollHeartbeat
				if json.Unmarshal(decoded, &hb) == nil {
					rep := processIncomingEnroll(hb)
					b, _ := json.Marshal(rep)
					_, _ = conn.Write(append(b, '\n'))
					return
				}
			}
		}
	}

	if strings.HasPrefix(targetPort, "probe:") {
		_, _ = conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
		return
	}

	target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()

	_, _ = conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		_, _ = br.Read(buf)
		_, _ = target.Write(buf)
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(target, &appRxBytes, mbLimit)
	rxW := newThrottledWriter(conn, &appTxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(txW, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(rxW, target); done <- struct{}{} }()
	<-done
}
