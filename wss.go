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
	"math/big"
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

func newThrottledWriter(w io.Writer, counter *uint64, mbPerSec float64) io.Writer {
	limit := int64(mbPerSec * 1024.0 * 1024.0)
	return &throttledWriter{
		w:                w,
		counter:          counter,
		bytesLimitPerSec: limit,
		lastSecStart:     time.Now(),
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

func startWSSEngine(t Tunnel) {
	mu.Lock()
	stopTunnelIfRunning(t.ID)
	mu.Unlock()

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
	rand.Read(keyBytes)
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
		br.Read(buf)
		client.Write(buf)
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(tlsConn, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, client); done <- struct{}{} }()
	go func() { io.Copy(rxW, tlsConn); done <- struct{}{} }()
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
		} else if strings.HasPrefix(low, "x-enroll-payload:") {
			rawB64 := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if decoded, err := base64.StdEncoding.DecodeString(rawB64); err == nil {
				var hb EnrollHeartbeat
				if json.Unmarshal(decoded, &hb) == nil {
					rep := processIncomingEnroll(hb)
					b, _ := json.Marshal(rep)
					conn.Write(append(b, '\n'))
					return
				}
			}
		}
	}

	// Decoupled Ping Probe: Return 101 Switching Protocols to verify pure tunnel health
	if strings.HasPrefix(targetPort, "probe:") {
		conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
		return
	}

	target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()

	conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWSAccept(wsKey))))
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(target, &appRxBytes, mbLimit)
	rxW := newThrottledWriter(conn, &appTxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, conn); done <- struct{}{} }()
	go func() { io.Copy(rxW, target); done <- struct{}{} }()
	<-done
}

// Tests end-to-end tunnel health independently of any downstream proxy service
func probeTunnelOnce(tun Tunnel) PingPoint {
	ports := parsePortList(tun.InboundPorts)
	firstPort := "2053"
	if len(ports) > 0 {
		firstPort = ports[0]
	}
	pt := PingPoint{Timestamp: time.Now().Unix(), TunnelID: tun.ID, ServerID: tun.TargetServer}

	if tun.Method == "forward" {
		rttStart := time.Now()
		c, err := net.DialTimeout("tcp", tun.RemoteAddr, 4*time.Second)
		if err != nil {
			pt.OK = false
			pt.Error = "Tunnel Port Closed"
			recordPingPoint(pt)
			return pt
		}
		defer c.Close()
		c.Write([]byte("FWD|probe:" + firstPort + "\n"))
		reply, err := bufio.NewReader(c).ReadString('\n')
		pt.LatencyMS = time.Since(rttStart).Milliseconds()
		pt.OK = (err == nil && strings.HasPrefix(reply, "OK")) || err == nil
		if !pt.OK {
			pt.Error = "Handshake Rejected"
		}
		recordPingPoint(pt)
		return pt
	}

	rttStart := time.Now()
	tlsCfg := buildTunnelTLSConfig(tun)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
	if err != nil {
		pt.OK = false
		pt.Error = "Tunnel Offline"
		recordPingPoint(pt)
		return pt
	}
	defer conn.Close()

	if tun.Method == "direct" || tun.Method == "spoof" {
		conn.Write([]byte("LTUN|probe:" + firstPort + "\n"))
		reply, err := bufio.NewReader(conn).ReadString('\n')
		pt.LatencyMS = time.Since(rttStart).Milliseconds()
		pt.OK = (err == nil && strings.HasPrefix(reply, "OK")) || err == nil
		if !pt.OK {
			pt.Error = "Tunnel Unresponsive"
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
		pt.Error = "Upgrade Rejected"
	}
	recordPingPoint(pt)
	return pt
}

// Relaxed automated background probe: runs every ~5 minutes (270s-330s with jitter)
func startPingMonitor() {
	for {
		jitterBig, _ := rand.Int(rand.Reader, big.NewInt(60))
		sleepSec := 270 + int(jitterBig.Int64()) // 4.5 to 5.5 minutes
		time.Sleep(time.Duration(sleepSec) * time.Second)

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
