package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type throttledWriter struct {
	w                io.Writer
	counter          *uint64
	bytesLimitPerSec int64
	bytesSentThisSec int64
	lastSecStart     time.Time
}

func newThrottledWriter(w io.Writer, counter *uint64, mbPerSec int) io.Writer {
	limit := int64(mbPerSec) * 1024 * 1024
	return &throttledWriter{
		w:                w,
		counter:          counter,
		bytesLimitPerSec: limit,
		lastSecStart:     time.Now(),
	}
}

func (t *throttledWriter) Write(p []byte) (int, error) {
	if t.bytesLimitPerSec <= 0 {
		n, err := t.w.Write(p)
		if n > 0 {
			atomic.AddUint64(t.counter, uint64(n))
		}
		return n, err
	}
	total := 0
	for len(p) > 0 {
		now := time.Now()
		if now.Sub(t.lastSecStart) >= time.Second {
			t.bytesSentThisSec = 0
			t.lastSecStart = now
		}
		avail := t.bytesLimitPerSec - t.bytesSentThisSec
		if avail <= 0 {
			time.Sleep(15 * time.Millisecond)
			continue
		}
		chunk := int64(len(p))
		if chunk > avail {
			chunk = avail
		}
		n, err := t.w.Write(p[:chunk])
		total += n
		if n > 0 {
			t.bytesSentThisSec += int64(n)
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

	if t.Method == "" || t.Method == "spoof" {
		if t.Method == "spoof" {
			t.Method = "tls"
		} else {
			t.Method = "wss"
		}
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
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{localCert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err := tls.Listen("tcp", ":"+tunPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Exit %s]: Cannot bind TLS Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [%s-Exit %s]: TLS :%s -> X-UI 127.0.0.1:[%s]", strings.ToUpper(t.Method), t.Name, tunPort, strings.Join(inboundPorts, ",")))

		go func(l net.Listener, defPort string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go handleServerConn(conn, t, defPort)
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

		addLog(fmt.Sprintf("LISTEN [%s-Entry %s]: :%s -> %s (SNI: %s)", strings.ToUpper(t.Method), t.Name, iPort, t.RemoteAddr, t.SNI))

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

	// METHOD 2: DIRECT TLS STREAM RELAY (Zero HTTP/WS overhead, optional TCP segmentation)
	if t.Method == "tls" || t.Method == "spoof" {
		fragSz, _ := strconv.Atoi(t.FragSize)
		var baseConn net.Conn = rawConn
		if fragSz > 0 {
			baseConn = &tcpSegmentConn{Conn: rawConn, fragSize: fragSz}
		}
		tlsConn := tls.Client(baseConn, buildTunnelTLSConfig(t))
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
			return nil, nil, fmt.Errorf("direct TLS relay rejected: %s", strings.TrimSpace(reply))
		}
		return tlsConn, br, nil
	}

	// METHOD 1: WSS (WebSocket over TLS 1.3)
	tlsConn := tls.Client(rawConn, buildTunnelTLSConfig(t))
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

	mbLimit, _ := strconv.Atoi(t.SpeedLimit)
	txW := newThrottledWriter(remoteTLS, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, client); done <- struct{}{} }()
	go func() { io.Copy(rxW, remoteTLS); done <- struct{}{} }()
	<-done
}

func handleServerConn(conn net.Conn, t Tunnel, defaultPort string) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	firstLine, err := br.ReadString('\n')
	if err != nil {
		return
	}

	// Method 2: Direct TLS Stream ("LTUN|<port>\n")
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
		pipeStreams(conn, target, br, t.SpeedLimit)
		return
	}

	// Method 1: WSS Stream
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
	pipeStreams(conn, target, br, t.SpeedLimit)
}

func pipeStreams(conn net.Conn, target net.Conn, br *bufio.Reader, speedLimit string) {
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}
	mbLimit, _ := strconv.Atoi(speedLimit)
	txW := newThrottledWriter(target, &appRxBytes, mbLimit)
	rxW := newThrottledWriter(conn, &appTxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, conn); done <- struct{}{} }()
	go func() { io.Copy(rxW, target); done <- struct{}{} }()
	<-done
}

func probeTunnelOnce(tun Tunnel) PingPoint {
	ports := parsePortList(tun.InboundPorts)
	firstPort := "2053"
	if len(ports) > 0 {
		firstPort = ports[0]
	}
	pt := PingPoint{Timestamp: time.Now().Unix(), TunnelID: tun.ID, ServerID: tun.TargetServer}

	tlsCfg := buildTunnelTLSConfig(tun)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
	if err != nil {
		pt.OK = false
		pt.Error = "Tunnel Offline"
		recordPingPoint(pt)
		return pt
	}
	defer conn.Close()

	rttStart := time.Now()
	if tun.Method == "tls" || tun.Method == "spoof" {
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
