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
	"time"
)

func computeWSAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Clean comma-separated port list and strip any accidental IP prefix (e.g. "127.0.0.1:2053" -> "2053")
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

	xuiPorts := parsePortList(t.XUIPort)
	if len(xuiPorts) == 0 {
		xuiPorts = []string{"2053"}
	}

	// EXIT SERVER ROLE: Listen on single WSS Bridge Port and route to 127.0.0.1:<X-Target-Port>
	if t.Role == "server" {
		wssPort := strings.TrimSpace(t.WSSPort)
		if wssPort == "" {
			wssPort = parsePortList(t.ListenPort)[0]
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{localCert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err := tls.Listen("tcp", ":"+wssPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Exit %s]: Cannot bind :%s -> %v", t.Name, wssPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [WSS-Exit %s]: TLS :%s -> 127.0.0.1:[%s]", t.Name, wssPort, strings.Join(xuiPorts, ",")))

		go func(l net.Listener, defaultXUI string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go handleWSSServerConn(conn, t, defaultXUI)
			}
		}(ln, xuiPorts[0])
		return
	}

	// ENTRY SERVER ROLE: Listen on 1 or multiple comma-separated Entry Ports
	entryPorts := parsePortList(t.ListenPort)
	if len(entryPorts) == 0 && len(xuiPorts) > 0 {
		entryPorts = xuiPorts
	}

	for idx, ePort := range entryPorts {
		targetXUI := xuiPorts[0]
		if idx < len(xuiPorts) {
			targetXUI = xuiPorts[idx]
		}

		ln, err := net.Listen("tcp", ":"+ePort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Entry %s]: Cannot bind :%s -> %v", t.Name, ePort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [WSS-Entry %s]: :%s -> wss://%s%s (X-UI :%s)", t.Name, ePort, t.RemoteAddr, t.WSPath, targetXUI))

		go func(l net.Listener, xPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				go handleWSSClientConn(client, t, xPort)
			}
		}(ln, targetXUI)
	}
}

func handleWSSClientConn(client net.Conn, t Tunnel, xuiPort string) {
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

	dialer := &net.Dialer{Timeout: 6 * time.Second}
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
		t.WSPath, t.SNI, wsKey, xuiPort)

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

	go io.Copy(remoteTLS, client)
	io.Copy(client, remoteTLS)
}

func handleWSSServerConn(conn net.Conn, t Tunnel, defaultXUI string) {
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
	targetPort := defaultXUI
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

	accept := computeWSAccept(wsKey)
	resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := conn.Write([]byte(resp)); err != nil {
		return
	}

	// If this is a Ping test probe, close cleanly after 101 handshake
	if targetPort == "ping" {
		return
	}

	localXUI := "127.0.0.1:" + strings.TrimPrefix(targetPort, "127.0.0.1:")
	target, err := net.DialTimeout("tcp", localXUI, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("X-UI ERR [%s]: Local X-UI port %s refused connection (%v)", t.Name, localXUI, err))
		return
	}
	defer target.Close()

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}

	go io.Copy(target, conn)
	io.Copy(conn, target)
}
