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

func stopTunnelIfRunning(id string) {
	if ln, ok := activeListeners[id]; ok {
		ln.Close()
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

	if t.Role == "server" {
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{localCert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err := tls.Listen("tcp", ":"+t.ListenPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Server %s]: Cannot bind :%s -> %v", t.Name, t.ListenPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = ln
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [WSS-Server %s]: TLS :%s (Path %s) -> %s", t.Name, t.ListenPort, t.WSPath, t.TargetAddr))

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleWSSServerConn(conn, t)
		}
	}

	ln, err := net.Listen("tcp", ":"+t.ListenPort)
	if err != nil {
		addLog(fmt.Sprintf("ERR [WSS-Client %s]: Cannot bind :%s -> %v", t.Name, t.ListenPort, err))
		return
	}
	mu.Lock()
	activeListeners[t.ID] = ln
	mu.Unlock()
	addLog(fmt.Sprintf("LISTEN [WSS-Client %s]: TCP :%s -> wss://%s%s (SNI: %s)", t.Name, t.ListenPort, t.TargetAddr, t.WSPath, t.SNI))

	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		go handleWSSClientConn(client, t)
	}
}

func handleWSSClientConn(client net.Conn, t Tunnel) {
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
	remoteTLS, err := tls.DialWithDialer(dialer, "tcp", t.TargetAddr, tlsCfg)
	if err != nil {
		addLog(fmt.Sprintf("ERR [%s]: TLS Dial failed -> %v", t.Name, err))
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
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36\r\n\r\n",
		t.WSPath, t.SNI, wsKey)

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

func handleWSSServerConn(conn net.Conn, t Tunnel) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	parts := strings.Fields(reqLine)
	if len(parts) < 2 || parts[1] != t.WSPath {
		decoy := "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nConnection: close\r\n\r\n<html><body><h1>200 OK</h1><p>Edge Service Online</p></body></html>"
		conn.Write([]byte(decoy))
		return
	}

	var wsKey string
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-key:") {
			wsKey = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
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

	targetAddr := t.TargetAddr
	if !strings.Contains(targetAddr, ":") {
		targetAddr = targetAddr + ":2053"
	}

	target, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("WARN [%s]: WSS Handshake OK, but X-UI port %s not listening.", t.Name, targetAddr))
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
