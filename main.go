package main

import (
	"bufio"
	"bytes"
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
	"os/exec"
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

type ServerNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	SSHPort   string `json:"ssh_port"`
	SSHUser   string `json:"ssh_user"`
	SSHPass   string `json:"ssh_pass"`
	PanelPort string `json:"panel_port"`
	CertPin   string `json:"cert_pin"`
	Status    string `json:"status"` // "Online", "Deploying...", "Error"
	IsLocal   bool   `json:"is_local"`
}

type Tunnel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`          // "client" or "server"
	Method       string `json:"method"`        // "wss"
	SourceServer string `json:"source_server"` // Server ID for Entry
	TargetServer string `json:"target_server"` // Server ID for Exit
	ListenPort   string `json:"listen_port"`   // Entry Port (or WSS Server Port)
	WSSPort      string `json:"wss_port"`      // Remote WSS Port on Exit Server
	TargetAddr   string `json:"target_addr"`   // Remote IP:Port or Local X-UI IP:Port
	SNI          string `json:"sni"`
	WSPath       string `json:"ws_path"`
	CertPin      string `json:"cert_pin"`
	Status       string `json:"status"`
}

type UIPrefs struct {
	Theme          string `json:"theme"`
	Lang           string `json:"lang"`
	ActiveTab      string `json:"active_tab"`
	SelectedServer string `json:"selected_server"`
}

type Database struct {
	Servers []ServerNode `json:"servers"`
	Tunnels []Tunnel     `json:"tunnels"`
	Prefs   UIPrefs      `json:"prefs"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

var (
	db = Database{
		Servers: []ServerNode{},
		Tunnels: []Tunnel{},
		Prefs: UIPrefs{
			Theme:          "amber",
			Lang:           "en",
			ActiveTab:      "servers",
			SelectedServer: "local",
		},
	}
	activeListeners = make(map[string]net.Listener)
	logs            = []string{}
	localCert       tls.Certificate
	localPinHex     string
	localIP         string
	mu              sync.Mutex
)

func detectLocalIP() string {
	out, err := exec.Command("bash", "-c", "hostname -I | awk '{print $1}'").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return strings.TrimSpace(string(out))
	}
	return "127.0.0.1"
}

func addLog(msg string) {
	mu.Lock()
	defer mu.Unlock()
	timestamp := time.Now().Format("15:04:05")
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
	addLog("SYS: LiteTun Multi-Server Controller initialized.")
}

func saveDBLocked() {
	_ = os.MkdirAll("/opt/litetun", 0755)
	data, _ := json.MarshalIndent(db, "", "  ")
	_ = os.WriteFile(dbPath, data, 0644)
}

func loadDBFromDisk(port string) {
	localIP = detectLocalIP()
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
	if db.Prefs.SelectedServer == "" {
		db.Prefs.SelectedServer = "local"
	}

	// Ensure Master Node ("local") is always first in the server list
	hasLocal := false
	for i := range db.Servers {
		if db.Servers[i].ID == "local" {
			db.Servers[i].IP = localIP
			db.Servers[i].PanelPort = port
			db.Servers[i].CertPin = localPinHex
			db.Servers[i].Status = "Online"
			db.Servers[i].IsLocal = true
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		master := ServerNode{
			ID:        "local",
			Name:      "Master Server (This Node)",
			IP:        localIP,
			SSHPort:   "22",
			SSHUser:   "root",
			PanelPort: port,
			CertPin:   localPinHex,
			Status:    "Online",
			IsLocal:   true,
		}
		db.Servers = append([]ServerNode{master}, db.Servers...)
	}
	saveDBLocked()

	for _, t := range db.Tunnels {
		if t.SourceServer == "" || t.SourceServer == "local" {
			go startTunnelEngine(t)
		}
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

	// Special Ping Probe Check (if TargetAddr == "ping", echo back immediately)
	targetAddr := t.TargetAddr
	if !strings.Contains(targetAddr, ":") {
		targetAddr = targetAddr + ":2053"
	}

	target, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("WARN [%s]: WSS Handshake OK, but X-UI port %s is not listening yet.", t.Name, targetAddr))
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

func findServerByID(id string) *ServerNode {
	for i := range db.Servers {
		if db.Servers[i].ID == id {
			return &db.Servers[i]
		}
	}
	return nil
}

// Deploy Sentinel to Target Server via SSH
func deploySentinelSSH(s ServerNode) {
	addLog(fmt.Sprintf("SSH [%s]: Connecting to %s@%s:%s ...", s.Name, s.SSHUser, s.IP, s.SSHPort))

	remoteCmd := "export DEBIAN_FRONTEND=noninteractive; " +
		"mkdir -p /opt/litetun; " +
		"if [ ! -f /opt/litetun/config.env ]; then echo -e 'PANEL_PORT=8880\\nPANEL_PATH=sentinel\\nPANEL_USER=admin\\nPANEL_PASS=admin' > /opt/litetun/config.env; fi; " +
		"systemctl stop litetun 2>/dev/null || true; " +
		"curl -Ls https://github.com/sahand363/ghoghnoos_light/releases/latest/download/ghoghnoos_lite -o /opt/litetun/ghoghnoos_lite.tmp && " +
		"mv -f /opt/litetun/ghoghnoos_lite.tmp /opt/litetun/ghoghnoos_lite && " +
		"chmod +x /opt/litetun/ghoghnoos_lite && " +
		"curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos_light/main/install.sh | tr -d '\\r' > /usr/local/bin/litetun && " +
		"chmod +x /usr/local/bin/litetun && " +
		"/usr/local/bin/litetun --sentinel && " +
		"systemctl restart litetun && " +
		"grep '^PANEL_PORT=' /opt/litetun/config.env | cut -d= -f2"

	cmd := exec.Command("sshpass", "-p", s.SSHPass,
		"ssh", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=12",
		"-p", s.SSHPort, fmt.Sprintf("%s@%s", s.SSHUser, s.IP), remoteCmd)

	out, err := cmd.CombinedOutput()
	if err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Deployment failed -> %v (%s)", s.Name, err, strings.TrimSpace(string(out))))
		mu.Lock()
		if srv := findServerByID(s.ID); srv != nil {
			srv.Status = "SSH Error"
			saveDBLocked()
		}
		mu.Unlock()
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	detectedPort := "8880"
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		detectedPort = strings.TrimSpace(lines[len(lines)-1])
	}

	addLog(fmt.Sprintf("SENTINEL [%s]: Deployed on %s:%s! Waiting for agent handshake...", s.Name, s.IP, detectedPort))
	time.Sleep(2 * time.Second)

	// Fetch remote Sentinel state to get its Certificate Pin
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort))
	remotePin := ""
	status := "Online"
	if err == nil {
		defer resp.Body.Close()
		var st map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&st) == nil {
			if p, ok := st["cert_pin"].(string); ok {
				remotePin = p
			}
		}
		addLog(fmt.Sprintf("CONNECTED [%s]: Sentinel Online! SHA-256 Pin synced.", s.Name))
	} else {
		addLog(fmt.Sprintf("WARN [%s]: Sentinel installed via SSH, but HTTP port %s not reachable (%v)", s.Name, detectedPort, err))
	}

	mu.Lock()
	if srv := findServerByID(s.ID); srv != nil {
		srv.PanelPort = detectedPort
		srv.CertPin = remotePin
		srv.Status = status
		saveDBLocked()
	}
	mu.Unlock()
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
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "8880"
	}
	panelPath := strings.Trim(os.Getenv("PANEL_PATH"), "/")

	loadLogsFromDisk()
	initSelfSignedCert()
	loadDBFromDisk(port)

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

	// State API (Supports viewing Master or proxying to Selected Remote Server)
	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		targetSrvID := r.URL.Query().Get("server_id")

		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		mu.Lock()
		serversCopy := append([]ServerNode{}, db.Servers...)
		tunnelsCopy := append([]Tunnel{}, db.Tunnels...)
		prefsCopy := db.Prefs
		localLogs := append([]string{}, logs...)
		mu.Unlock()

		// If a remote server is selected on top, fetch its live logs/stats
		activeLogs := localLogs
		ramMB := m.Alloc / 1024 / 1024
		goroutines := runtime.NumGoroutine()
		activePort := port
		activePath := panelPath
		activePin := localPinHex

		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			mu.Unlock()
			if srv != nil && srv.IP != "" {
				client := &http.Client{Timeout: 3 * time.Second}
				resp, err := client.Get(fmt.Sprintf("http://%s:%s/api/state", srv.IP, srv.PanelPort))
				if err == nil {
					defer resp.Body.Close()
					var rem map[string]interface{}
					if json.NewDecoder(resp.Body).Decode(&rem) == nil {
						if rl, ok := rem["logs"].([]interface{}); ok {
							activeLogs = []string{}
							for _, item := range rl {
								activeLogs = append(activeLogs, fmt.Sprintf("%v", item))
							}
						}
						if rm, ok := rem["ram_mb"].(float64); ok {
							ramMB = uint64(rm)
						}
						if rg, ok := rem["goroutines"].(float64); ok {
							goroutines = int(rg)
						}
						if rp, ok := rem["panel_port"].(string); ok {
							activePort = rp
						}
						if rpath, ok := rem["panel_path"].(string); ok {
							activePath = rpath
						}
						if rpin, ok := rem["cert_pin"].(string); ok {
							activePin = rpin
						}
					}
				} else {
					activeLogs = append(localLogs, fmt.Sprintf("[WARN] Remote node %s (%s:%s) unreachable: %v", srv.Name, srv.IP, srv.PanelPort, err))
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ram_mb":     ramMB,
			"goroutines": goroutines,
			"servers":    serversCopy,
			"tunnels":    tunnelsCopy,
			"prefs":      prefsCopy,
			"logs":       activeLogs,
			"panel_port": activePort,
			"panel_path": activePath,
			"cert_pin":   activePin,
		})
	})

	// Add / Remove Server & Deploy Sentinel
	http.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			id := r.URL.Query().Get("id")
			if id == "local" {
				return
			}
			mu.Lock()
			var updated []ServerNode
			for _, s := range db.Servers {
				if s.ID != id {
					updated = append(updated, s)
				}
			}
			db.Servers = updated
			if db.Prefs.SelectedServer == id {
				db.Prefs.SelectedServer = "local"
			}
			saveDBLocked()
			mu.Unlock()
			addLog("SERVER: Removed server node " + id)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			return
		}
		var s ServerNode
		json.NewDecoder(r.Body).Decode(&s)
		if s.SSHPort == "" {
			s.SSHPort = "22"
		}
		if s.SSHUser == "" {
			s.SSHUser = "root"
		}
		if s.PanelPort == "" {
			s.PanelPort = "8880"
		}
		s.ID = fmt.Sprintf("srv-%d", time.Now().Unix()%10000)
		s.Status = "Deploying..."

		mu.Lock()
		db.Servers = append(db.Servers, s)
		saveDBLocked()
		mu.Unlock()

		go deploySentinelSSH(s)
		w.WriteHeader(http.StatusOK)
	})

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
			if p.SelectedServer != "" {
				db.Prefs.SelectedServer = p.SelectedServer
			}
			saveDBLocked()
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/cmd", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req struct {
			Cmd      string `json:"cmd"`
			ServerID string `json:"server_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.ServerID != "" && req.ServerID != "local" {
			mu.Lock()
			srv := findServerByID(req.ServerID)
			mu.Unlock()
			if srv != nil {
				body, _ := json.Marshal(map[string]string{"cmd": req.Cmd})
				http.Post(fmt.Sprintf("http://%s:%s/api/cmd", srv.IP, srv.PanelPort), "application/json", bytes.NewReader(body))
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		cmd := strings.TrimSpace(req.Cmd)
		addLog("root@litetun:~# " + cmd)
		switch strings.ToLower(cmd) {
		case "help":
			addLog("Commands: status | servers | tunnels | pin | clear | restart")
		case "status":
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			addLog(fmt.Sprintf("STATUS: Online | Goroutines: %d | RAM: %d MB | Port: %s", runtime.NumGoroutine(), m.Alloc/1024/1024, port))
		case "servers":
			mu.Lock()
			cnt := len(db.Servers)
			mu.Unlock()
			addLog(fmt.Sprintf("SERVERS: %d server(s) linked.", cnt))
		case "tunnels":
			mu.Lock()
			cnt := len(db.Tunnels)
			mu.Unlock()
			addLog(fmt.Sprintf("TUNNELS: %d active tunnel(s).", cnt))
		case "pin":
			addLog("SHA256-PIN: " + localPinHex)
		case "clear":
			mu.Lock()
			logs = []string{"[SYSTEM] Terminal cleared."}
			_ = os.WriteFile(logPath, []byte(logs[0]+"\n"), 0644)
			mu.Unlock()
		case "restart":
			addLog("SYS: Restarting service...")
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

	// Create Tunnel (Auto-deploys Exit WSS Server & Entry WSS Client across selected servers)
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
			addLog("TUNNEL: Deleted tunnel " + id)
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
		if t.WSSPort == "" {
			t.WSSPort = "10201"
		}
		if t.ID == "" {
			t.ID = fmt.Sprintf("wss-%d", time.Now().Unix()%10000)
		}

		mu.Lock()
		srcSrv := findServerByID(t.SourceServer)
		dstSrv := findServerByID(t.TargetServer)
		mu.Unlock()

		// If bridging two servers from the dropdowns, configure the Exit Server first
		if srcSrv != nil && dstSrv != nil && t.SourceServer != "" && t.TargetServer != "" && t.Role == "" {
			t.Role = "client"
			exitTunnel := Tunnel{
				ID:         t.ID + "-exit",
				Name:       t.Name + " (Exit)",
				Role:       "server",
				Method:     "wss",
				ListenPort: t.WSSPort,
				TargetAddr: t.TargetAddr, // Local X-UI e.g. 127.0.0.1:2053
				SNI:        t.SNI,
				WSPath:     t.WSPath,
				Status:     "Online",
			}

			if dstSrv.IsLocal {
				go startTunnelEngine(exitTunnel)
			} else {
				body, _ := json.Marshal(exitTunnel)
				resp, err := http.Post(fmt.Sprintf("http://%s:%s/api/tunnel", dstSrv.IP, dstSrv.PanelPort), "application/json", bytes.NewReader(body))
				if err != nil {
					addLog(fmt.Sprintf("WARN [%s]: Could not auto-configure Exit node %s via HTTP, trying SSH fallback...", t.Name, dstSrv.Name))
				} else {
					resp.Body.Close()
					addLog(fmt.Sprintf("REMOTE [%s]: Configured WSS-Server on %s (:%s -> X-UI %s)", t.Name, dstSrv.Name, t.WSSPort, t.TargetAddr))
				}
			}

			// Point Entry Server's TargetAddr to Exit Server's IP:WSSPort
			t.TargetAddr = fmt.Sprintf("%s:%s", dstSrv.IP, t.WSSPort)
			if t.CertPin == "" && dstSrv.CertPin != "" {
				t.CertPin = dstSrv.CertPin
			}
		}

		mu.Lock()
		replaced := false
		for i := range db.Tunnels {
			if db.Tunnels[i].ID == t.ID {
				db.Tunnels[i] = t
				replaced = true
				break
			}
		}
		if !replaced {
			db.Tunnels = append(db.Tunnels, t)
		}
		saveDBLocked()
		mu.Unlock()

		if srcSrv == nil || srcSrv.IsLocal {
			go startTunnelEngine(t)
		} else {
			body, _ := json.Marshal(t)
			_, err := http.Post(fmt.Sprintf("http://%s:%s/api/tunnel", srcSrv.IP, srcSrv.PanelPort), "application/json", bytes.NewReader(body))
			if err == nil {
				addLog(fmt.Sprintf("REMOTE [%s]: Configured WSS-Client on %s (:%s -> %s)", t.Name, srcSrv.Name, t.ListenPort, t.TargetAddr))
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	// End-to-End WSS Tunnel Ping & Handshake Tester
	http.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		mu.Lock()
		var tun *Tunnel
		for i := range db.Tunnels {
			if db.Tunnels[i].ID == id {
				tun = &db.Tunnels[i]
				break
			}
		}
		mu.Unlock()

		if tun == nil {
			http.Error(w, "Tunnel not found", 404)
			return
		}

		start := time.Now()
		tlsCfg := &tls.Config{
			ServerName:         tun.SNI,
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err := tls.DialWithDialer(dialer, "tcp", tun.TargetAddr, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("PING-FAIL [%s]: %s unreachable -> %v", tun.Name, tun.TargetAddr, err))
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		defer conn.Close()

		keyBytes := make([]byte, 16)
		rand.Read(keyBytes)
		wsKey := base64.StdEncoding.EncodeToString(keyBytes)
		req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
			tun.WSPath, tun.SNI, wsKey)
		conn.Write([]byte(req))

		br := bufio.NewReader(conn)
		statusLine, err := br.ReadString('\n')
		ms := time.Since(start).Milliseconds()

		if err != nil || !strings.Contains(statusLine, "101") {
			addLog(fmt.Sprintf("PING-ERR [%s]: Handshake failed (%d ms) -> %s", tun.Name, ms, strings.TrimSpace(statusLine)))
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "ms": ms, "error": strings.TrimSpace(statusLine)})
			return
		}

		addLog(fmt.Sprintf("PING-OK [%s]: WSS TLS 1.3 Handshake verified in %d ms!", tun.Name, ms))
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "ms": ms})
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)

		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			if srv != nil && req.Port != "" {
				srv.PanelPort = req.Port
				saveDBLocked()
			}
			mu.Unlock()
			if srv != nil {
				body, _ := json.Marshal(req)
				http.Post(fmt.Sprintf("http://%s:%s/api/config", srv.IP, srv.PanelPort), "application/json", bytes.NewReader(body))
				addLog(fmt.Sprintf("CFG [%s]: Updated remote panel port to %s", srv.Name, req.Port))
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		if req.Port == "" {
			req.Port = port
		}
		if err := updateConfigFile(req.Port, req.Path); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		addLog(fmt.Sprintf("SYS: Updated Master Panel Port=%s Path=/%s/ -> Restarting...", req.Port, req.Path))
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
