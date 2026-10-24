package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
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

func computeWSAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func enableKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(15 * time.Second)
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
			addLog(fmt.Sprintf("ERR [WSS-Exit %s]: Cannot bind Tunnel Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [WSS-Exit %s]: Tunnel :%s -> X-UI 127.0.0.1:[%s]", t.Name, tunPort, strings.Join(inboundPorts, ",")))

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
			addLog(fmt.Sprintf("ERR [WSS-Entry %s]: Cannot bind Inbound Port :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [WSS-Entry %s]: :%s -> Tunnel %s (X-UI :%s)", t.Name, iPort, t.RemoteAddr, iPort))

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

	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))
	tlsCfg := &tls.Config{
		ServerName:         t.SNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}

	if pinClean != "" {
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			actual := hex.EncodeToString(sum[:])
			if actual != pinClean {
				addLog(fmt.Sprintf("SEC-ALERT [%s]: Cert Pin mismatch!", t.Name))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}

	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	remoteTLS, err := tls.DialWithDialer(dialer, "tcp", t.RemoteAddr, tlsCfg)
	if err != nil {
		addLog(fmt.Sprintf("ERR [%s]: Dial %s failed -> %v", t.Name, t.RemoteAddr, err))
		return
	}
	defer remoteTLS.Close()

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"X-Target-Port: %s\r\n"+
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36\r\n\r\n",
		t.WSPath, t.SNI, wsKey, targetPort)

	if _, err := remoteTLS.Write([]byte(req)); err != nil {
		return
	}

	br := bufio.NewReader(remoteTLS)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		addLog(fmt.Sprintf("ERR [%s]: WSS Upgrade rejected -> %s", t.Name, strings.TrimSpace(statusLine)))
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

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(&byteCounterWriter{w: remoteTLS, counter: &appTxBytes}, client)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(&byteCounterWriter{w: client, counter: &appRxBytes}, remoteTLS)
		done <- struct{}{}
	}()
	<-done
}

func handleWSSServerConn(conn net.Conn, t Tunnel, defaultPort string) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	parts := strings.Fields(reqLine)
	if len(parts) < 2 || parts[1] != t.WSPath {
		decoy := "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nConnection: close\r\n\r\n<html><body><h1>200 OK</h1></body></html>"
		conn.Write([]byte(decoy))
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
			p := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if p != "" {
				targetPort = p
			}
		}
	}

	if strings.HasPrefix(targetPort, "probe:") {
		probePort := strings.TrimPrefix(targetPort, "probe:")
		testConn, err := net.DialTimeout("tcp", "127.0.0.1:"+probePort, 2*time.Second)
		if err != nil {
			conn.Write([]byte("HTTP/1.1 502 X-UI Port " + probePort + " Unreachable\r\n\r\n"))
			return
		}
		testConn.Close()
		accept := computeWSAccept(wsKey)
		conn.Write([]byte(fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)))
		return
	}

	localXUI := "127.0.0.1:" + strings.TrimPrefix(targetPort, "127.0.0.1:")
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	target, err := dialer.Dial("tcp", localXUI)
	if err != nil {
		addLog(fmt.Sprintf("X-UI ERR [%s]: Local X-UI %s refused (%v)", t.Name, localXUI, err))
		return
	}
	defer target.Close()

	accept := computeWSAccept(wsKey)
	resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := conn.Write([]byte(resp)); err != nil {
		return
	}

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(&byteCounterWriter{w: target, counter: &appRxBytes}, conn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(&byteCounterWriter{w: conn, counter: &appTxBytes}, target)
		done <- struct{}{}
	}()
	<-done
}

// Measures true 1-trip RTT (~180-200ms) instead of 3-trip TCP+TLS+WS setup (~550ms)
func probeTunnelOnce(tun Tunnel) PingPoint {
	ports := parsePortList(tun.InboundPorts)
	firstPort := "2053"
	if len(ports) > 0 {
		firstPort = ports[0]
	}
	pt := PingPoint{Timestamp: time.Now().Unix(), TunnelID: tun.ID, ServerID: tun.TargetServer}

	tlsCfg := &tls.Config{ServerName: tun.SNI, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
	if err != nil {
		pt.OK = false
		pt.Error = "Tunnel Offline"
		recordPingPoint(pt)
		return pt
	}
	defer conn.Close()

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nX-Target-Port: probe:%s\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		tun.WSPath, tun.SNI, firstPort, wsKey)

	// Start RTT timer right when sending the probe packet over the established TLS channel
	rttStart := time.Now()
	if _, err := conn.Write([]byte(req)); err != nil {
		pt.OK = false
		pt.Error = "Write Error"
		recordPingPoint(pt)
		return pt
	}

	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	pt.LatencyMS = time.Since(rttStart).Milliseconds()

	if err != nil || !strings.Contains(statusLine, "101") {
		pt.OK = false
		pt.Error = "X-UI :" + firstPort + " Refused"
	} else {
		pt.OK = true
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
