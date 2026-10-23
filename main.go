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

const (
	wsGUID   = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	dbPath   = "/opt/litetun/panel_db.json"
	logPath  = "/opt/litetun/panel.log"
	confPath = "/opt/litetun/config.env"
)

type Tunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`        // "client" or "server"
	Method     string `json:"method"`      // "wss"
	ListenPort string `json:"listen_port"` // Local listen port
	TargetAddr string `json:"target_addr"` // Remote WSS IP:Port or Local X-UI IP:Port
	SNI        string `json:"sni"`
	WSPath     string `json:"ws_path"`
	CertPin    string `json:"cert_pin"`
	Status     string `json:"status"`
}

type UIPrefs struct {
	Theme     string `json:"theme"`
	Lang      string `json:"lang"`
	ActiveTab string `json:"active_tab"`
}

type Database struct {
	Tunnels []Tunnel `json:"tunnels"`
	Prefs   UIPrefs  `json:"prefs"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

var (
	db = Database{
		Tunnels: []Tunnel{},
		Prefs: UIPrefs{
			Theme:     "amber",
			Lang:      "en",
			ActiveTab: "servers",
		},
	}
	activeListeners = make(map[string]net.Listener)
	logs            = []string{}
	localCert       tls.Certificate
	localPinHex     string
	mu              sync.Mutex
)

func addLog(msg string) {
	mu.Lock()
	defer mu.Unlock()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s", timestamp, msg)
	logs = append(logs, line)
	if len(logs) > 250 {
		logs = logs[len(logs)-250:]
	}
	_ = os.MkdirAll("/opt/litetun", 0755)
	_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")+"\n"), 0644)
}

func loadLogsFromDisk() {
	data, err := os.ReadFile(logPath)
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			logs = lines
			if len(logs) > 250 {
				logs = logs[len(logs)-250:]
			}
		}
	}
	addLog("SYS: LiteTun daemon started and ready.")
}

func saveDBLocked() {
	_ = os.MkdirAll("/opt/litetun", 0755)
	data, _ := json.MarshalIndent(db, "", "  ")
	_ = os.WriteFile(dbPath, data, 0644)
}

func loadDBFromDisk() {
	// Migrate old tunnels.json if present
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if old, err := os.ReadFile("/opt/litetun/tunnels.json"); err == nil {
			_ = json.Unmarshal(old, &db.Tunnels)
			saveDBLocked()
		}
	}
	data, err := os.ReadFile(dbPath)
	if err == nil {
		_ = json.Unmarshal(data, &db)
	}
	if db.Prefs.Theme == "" {
		db.Prefs.Theme = "amber"
	}
	if db.Prefs.Lang == "" {
		db.Prefs.Lang = "en"
	}
	if db.Prefs.ActiveTab == "" {
		db.Prefs.ActiveTab = "servers"
	}
	for _, t := range db.Tunnels {
		go startTunnelEngine(t)
	}
}

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
	addLog(fmt.Sprintf("TLS: Generated ECDSA P-256 cert | SHA256 Pin: %s", localPinHex[:24]+"..."))
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
	addLog(fmt.Sprintf("CONN [%s]: Incoming client %s -> dialing WSS %s", t.Name, client.RemoteAddr().String(), t.TargetAddr))

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
				addLog(fmt.Sprintf("SEC-ALERT [%s]: Cert Pin mismatch! Expected %s", t.Name, pinClean[:16]+"..."))
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

	addLog(fmt.Sprintf("OK [%s]: 101 Switching Protocols established with %s", t.Name, t.TargetAddr))

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
		addLog(fmt.Sprintf("PROBE-BLOCKED [%s]: Invalid path %v from %s -> serving 200 OK decoy", t.Name, parts, conn.RemoteAddr().String()))
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
		addLog(fmt.Sprintf("ERR [%s]: Local X-UI target %s unreachable -> %v", t.Name, targetAddr, err))
		return
	}
	defer target.Close()

	addLog(fmt.Sprintf("BRIDGE [%s]: WSS stream bridged to local X-UI (%s)", t.Name, targetAddr))

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}

	go io.Copy(target, conn)
	io.Copy(conn, target)
}

func updateConfigFile(newPort, newPath string) error {
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
	loadLogsFromDisk()
	initSelfSignedCert()
	loadDBFromDisk()

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
			"goroutines": runtime.NumGoroutine(),
			"tunnels":    db.Tunnels,
			"prefs":      db.Prefs,
			"logs":       logs,
			"panel_port": port,
			"panel_path": panelPath,
			"cert_pin":   localPinHex,
		})
	})

	// Save Theme, Language, and Active Tab to Database
	http.HandleFunc("/api/prefs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var p UIPrefs
		if err := json.NewDecoder(r.Body).Decode(&p); err == nil {
			mu.Lock()
			if p.Theme != "" {
				db.Prefs.Theme = p.Theme
			}
			if p.Lang != "" {
				db.Prefs.Lang = p.Lang
			}
			if p.ActiveTab != "" {
				db.Prefs.ActiveTab = p.ActiveTab
			}
			saveDBLocked()
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})

	// Interactive Terminal Command Endpoint
	http.HandleFunc("/api/cmd", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req struct {
			Cmd string `json:"cmd"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		cmd := strings.TrimSpace(req.Cmd)
		addLog("root@litetun:~# " + cmd)

		switch strings.ToLower(cmd) {
		case "help":
			addLog("Commands: status | tunnels | pin | clear | restart")
		case "status":
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			addLog(fmt.Sprintf("STATUS: Active | Goroutines: %d | Alloc RAM: %d MB | Port: %s", runtime.NumGoroutine(), m.Alloc/1024/1024, port))
		case "tunnels":
			mu.Lock()
			count := len(db.Tunnels)
			mu.Unlock()
			addLog(fmt.Sprintf("TUNNELS: %d configured tunnel(s) running.", count))
		case "pin":
			addLog("SHA256-PIN: " + localPinHex)
		case "clear":
			mu.Lock()
			logs = []string{"[SYSTEM] Terminal buffer cleared."}
			_ = os.WriteFile(logPath, []byte(logs[0]+"\n"), 0644)
			mu.Unlock()
		case "restart":
			addLog("SYS: Restarting service via systemd...")
			go func() {
				time.Sleep(800 * time.Millisecond)
				os.Exit(0)
			}()
		default:
			if cmd != "" {
				addLog("bash: " + cmd + ": command not found (type 'help')")
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			id := r.URL.Query().Get("id")
			mu.Lock()
			stopTunnelIfRunning(id)
			var updated []Tunnel
			for _, item := range db.Tunnels {
				if item.ID != id {
					updated = append(updated, item)
				}
			}
			db.Tunnels = updated
			saveDBLocked()
			mu.Unlock()
			addLog("CMD: Deleted tunnel ID " + id)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			return
		}
		var t Tunnel
		json.NewDecoder(r.Body).Decode(&t)
		t.Status = "Online"
		if !strings.HasPrefix(t.WSPath, "/") {
			t.WSPath = "/" + strings.Trim(t.WSPath, "/")
		}

		mu.Lock()
		if t.ID == "" {
			t.ID = fmt.Sprintf("wss-%d", time.Now().Unix()%10000)
			db.Tunnels = append(db.Tunnels, t)
		} else {
			for i := range db.Tunnels {
				if db.Tunnels[i].ID == t.ID {
					db.Tunnels[i] = t
					break
				}
			}
		}
		saveDBLocked()
		mu.Unlock()

		addLog(fmt.Sprintf("CFG: Saved tunnel '%s' (%s) on port :%s", t.Name, t.Role, t.ListenPort))
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
		addLog(fmt.Sprintf("SYS: Updated Panel Port=%s Path=/%s/ -> Restarting...", req.Port, req.Path))
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
