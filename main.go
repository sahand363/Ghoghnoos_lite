package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type Tunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`        // "client" (Iran Relay -> WSS) or "server" (Kharej WSS -> X-UI)
	Method     string `json:"method"`      // "wss" or "tcp"
	ListenPort string `json:"listen_port"` // Port to listen on locally
	TargetAddr string `json:"target_addr"` // Remote WSS IP:Port (for client) OR Local X-UI 127.0.0.1:Port (for server)
	SNI        string `json:"sni"`         // TLS ServerName / Host header (e.g. speedtest.net)
	WSPath     string `json:"ws_path"`     // WebSocket path (e.g. /ws or /cdn-cgi/trace)
	CertPin    string `json:"cert_pin"`    // Optional SHA-256 Pin (hex)
	Status     string `json:"status"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

var (
	tunnels     = []Tunnel{}
	activeListeners = make(map[string]net.Listener)
	logs        = []string{"[SYSTEM] LiteTun WSS Engine initialized."}
	localCert   tls.Certificate
	localPinHex string
	mu          sync.Mutex
)

func addLog(msg string) {
	mu.Lock()
	defer mu.Unlock()
	timestamp := time.Now().Format("15:04:05")
	logs = append([]string{fmt.Sprintf("[%s] %s", timestamp, msg)}, logs...)
	if len(logs) > 80 {
		logs = logs[:80]
	}
}

// Generate an in-memory TLS certificate and compute its SHA-256 Pin for WSS Server mode
func initSelfSignedCert() {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "cloudflare-dns.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"cloudflare-dns.com", "localhost"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return
	}
	sum := sha256.Sum256(derBytes)
	localPinHex = hex.EncodeToString(sum[:])
	localCert = tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	addLog(fmt.Sprintf("WSS Local Cert SHA-256 Pin: %s", localPinHex))
}

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

	if t.WSPath == "" || !strings.HasPrefix(t.WSPath, "/") {
		t.WSPath = "/" + t.WSPath
	}
	if t.SNI == "" {
		t.SNI = "www.speedtest.net"
	}

	// MODE 1: WSS SERVER (Runs on Foreign/X-UI Server: Terminates WSS & forwards to local X-UI port)
	if t.Method == "wss" && t.Role == "server" {
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{localCert},
			MinVersion:   tls.VersionTLS13,
		}
		ln, err := tls.Listen("tcp", ":"+t.ListenPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("ERR [WSS-Server %s]: Failed bind :%s -> %v", t.Name, t.ListenPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = ln
		mu.Unlock()
		addLog(fmt.Sprintf("WSS-SERVER [%s] Listening on TLS :%s (Path: %s) -> X-UI %s", t.Name, t.ListenPort, t.WSPath, t.TargetAddr))

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleWSSServerConn(conn, t)
		}
	}

	// MODE 2: WSS CLIENT (Runs on Iran Relay Server: Listens TCP -> Wraps in TLS+WSS -> Sends to Foreign WSS Server)
	ln, err := net.Listen("tcp", ":"+t.ListenPort)
	if err != nil {
		addLog(fmt.Sprintf("ERR [%s]: Failed bind :%s -> %v", t.Name, t.ListenPort, err))
		return
	}
	mu.Lock()
	activeListeners[t.ID] = ln
	mu.Unlock()

	addLog(fmt.Sprintf("WSS-CLIENT [%s] Listening :%s -> wss://%s%s (SNI: %s)", t.Name, t.ListenPort, t.TargetAddr, t.WSPath, t.SNI))

	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		if t.Method == "wss" {
			go handleWSSClientConn(client, t)
		} else {
			go handleRawTCPConn(client, t)
		}
	}
}

func handleWSSClientConn(client net.Conn, t Tunnel) {
	defer client.Close()

	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))

	tlsCfg := &tls.Config{
		ServerName:         t.SNI,
		InsecureSkipVerify: true, // Verified manually via VerifyPeerCertificate if pin is set
		MinVersion:         tls.VersionTLS12,
	}

	if pinClean != "" {
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no server certificate presented")
			}
			sum := sha256.Sum256(rawCerts[0])
			actualPin := hex.EncodeToString(sum[:])
			if actualPin != pinClean {
				addLog(fmt.Sprintf("PIN MISMATCH [%s]: Expected %s, Got %s", t.Name, pinClean, actualPin))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}

	dialer := &net.Dialer{Timeout: 6 * time.Second}
	remoteTLS, err := tls.DialWithDialer(dialer, "tcp", t.TargetAddr, tlsCfg)
	if err != nil {
		addLog(fmt.Sprintf("WSS Dial Err [%s]: %v", t.Name, err))
		return
	}
	defer remoteTLS.Close()

	// Perform RFC 6455 WebSocket Upgrade Handshake over TLS
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
		addLog(fmt.Sprintf("WSS Handshake Rejected [%s]: %s", t.Name, strings.TrimSpace(statusLine)))
		return
	}
	// Consume remaining HTTP response headers
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
	}

	// Flush any buffered bytes after HTTP header
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
		// Return realistic decoy response against active probing / DPI inspectors
		decoy := "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nConnection: close\r\n\r\n<html><body><h1>200 OK</h1><p>Cloudflare Edge Server</p></body></html>"
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

	target, err := net.DialTimeout("tcp", t.TargetAddr, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("X-UI Connect Err [%s]: %v", t.Name, err))
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

func handleRawTCPConn(client net.Conn, t Tunnel) {
	defer client.Close()
	target, err := net.DialTimeout("tcp", t.TargetAddr, 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()
	go io.Copy(target, client)
	io.Copy(client, target)
}

func saveTunnelsToDisk() {
	data, _ := json.MarshalIndent(tunnels, "", "  ")
	_ = os.WriteFile("/opt/litetun/tunnels.json", data, 0644)
}

func loadTunnelsFromDisk() {
	data, err := os.ReadFile("/opt/litetun/tunnels.json")
	if err == nil {
		_ = json.Unmarshal(data, &tunnels)
		for _, t := range tunnels {
			go startTunnelEngine(t)
		}
	}
}

func updateConfigFile(newPort, newPath string) error {
	confPath := "/opt/litetun/config.env"
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "PANEL_PORT=") {
			lines[i] = "PANEL_PORT=" + newPort
		}
		if strings.HasPrefix(line, "PANEL_PATH=") {
			lines[i] = "PANEL_PATH=" + strings.Trim(newPath, "/")
		}
	}
	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
}

func main() {
	initSelfSignedCert()
	loadTunnelsFromDisk()

	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "8880"
	}
	panelPath := strings.Trim(os.Getenv("PANEL_PATH"), "/")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.Trim(r.URL.Path, "/")
		if panelPath != "" && reqPath != "" && reqPath != panelPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(indexHTML)
	})

	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ram_mb":     m.Alloc / 1024 / 1024,
			"tunnels":    tunnels,
			"logs":       logs,
			"panel_port": port,
			"panel_path": panelPath,
			"cert_pin":   localPinHex,
		})
	})

	// Create or Edit a WSS/TCP Tunnel
	http.HandleFunc("/api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			id := r.URL.Query().Get("id")
			mu.Lock()
			stopTunnelIfRunning(id)
			var updated []Tunnel
			for _, item := range tunnels {
				if item.ID != id {
					updated = append(updated, item)
				}
			}
			tunnels = updated
			saveTunnelsToDisk()
			mu.Unlock()
			addLog("DELETED Tunnel " + id)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			return
		}
		var t Tunnel
		json.NewDecoder(r.Body).Decode(&t)
		t.Status = "Online"

		mu.Lock()
		if t.ID == "" {
			t.ID = fmt.Sprintf("wss-%d", time.Now().Unix()%10000)
			tunnels = append(tunnels, t)
		} else {
			for i := range tunnels {
				if tunnels[i].ID == t.ID {
					tunnels[i] = t
					break
				}
			}
		}
		saveTunnelsToDisk()
		mu.Unlock()

		go startTunnelEngine(t)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)
		if req.Port == "" {
			req.Port = port
		}
		if err := updateConfigFile(req.Port, req.Path); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(1 * time.Second)
			os.Exit(0)
		}()
	})

	fmt.Printf("LiteTun running on port %s (Path: /%s/)\n", port, panelPath)
	if _, err := os.Stat("/opt/litetun/server.crt"); err == nil {
		http.ListenAndServeTLS(":"+port, "/opt/litetun/server.crt", "/opt/litetun/server.key", nil)
	} else {
		http.ListenAndServe(":"+port, nil)
	}
}
