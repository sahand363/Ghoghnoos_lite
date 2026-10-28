package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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
	Version   string `json:"version,omitempty"`
	Status    string `json:"status"`
	ViaMethod string `json:"via_method,omitempty"`
	Probing   bool   `json:"probing,omitempty"`
	LastSeen  int64  `json:"last_seen,omitempty"`
	IsLocal   bool   `json:"is_local"`
}

type Tunnel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Method       string `json:"method"`
	SourceServer string `json:"source_server"`
	TargetServer string `json:"target_server"`
	TunnelPort   string `json:"tunnel_port"`
	InboundPorts string `json:"inbound_ports"`
	RemoteAddr   string `json:"remote_addr"`
	SNI          string `json:"sni"`
	Fingerprint  string `json:"fingerprint"`
	FragSize     string `json:"frag_size"`
	SpeedLimit   string `json:"speed_limit"`
	WSPath       string `json:"ws_path"`
	CertPin      string `json:"cert_pin"`
	Status       string `json:"status"`
}

type PingPoint struct {
	Timestamp int64  `json:"ts"`
	TunnelID  string `json:"tid"`
	ServerID  string `json:"sid"`
	LatencyMS int64  `json:"ms"`
	OK        bool   `json:"ok"`
	Error     string `json:"err,omitempty"`
}

type DailyBW struct {
	Date string `json:"date"`
	RxMB uint64 `json:"rx_mb"`
	TxMB uint64 `json:"tx_mb"`
}

type SystemMetrics struct {
	CPUPercent float64   `json:"cpu_pct"`
	RAMUsedMB  uint64    `json:"ram_used_mb"`
	RAMTotalMB uint64    `json:"ram_total_mb"`
	RAMPct     uint64    `json:"ram_pct"`
	LiveRxMbps float64   `json:"live_rx_mbps"`
	LiveTxMbps float64   `json:"live_tx_mbps"`
	TpHistory  []float64 `json:"tp_history"`
	AppTotalMB uint64    `json:"app_total_mb"`
	SrvTotalGB float64   `json:"srv_total_gb"`
	BBRStatus  string    `json:"bbr_status"`
	Qdisc      string    `json:"qdisc"`
	DailyUsage []DailyBW `json:"daily_usage,omitempty"`
}

type UIPrefs struct {
	Theme          string `json:"theme"`
	Lang           string `json:"lang"`
	ActiveTab      string `json:"active_tab"`
	SelectedServer string `json:"selected_server"`
}

type Database struct {
	Servers        []ServerNode `json:"servers"`
	Tunnels        []Tunnel     `json:"tunnels"`
	PingHistory    []PingPoint  `json:"ping_history"`
	DailyUsage     []DailyBW    `json:"daily_usage"`
	LastRawRx      uint64       `json:"last_raw_rx"`
	LastRawTx      uint64       `json:"last_raw_tx"`
	AppBaseBytes   uint64       `json:"app_base_bytes"`
	EnrollToken    string       `json:"enroll_token"`
	CtrlL4Port     string       `json:"ctrl_l4_port"`
	CtrlDirectPort string       `json:"ctrl_direct_port"`
	CtrlWSSPort    string       `json:"ctrl_wss_port"`
	CtrlMethods    string       `json:"ctrl_methods"`
	Prefs          UIPrefs      `json:"prefs"`
}

type ConfigReq struct {
	Port        string `json:"port"`
	Path        string `json:"path"`
	User        string `json:"user"`
	Pass        string `json:"pass"`
	CtrlL4      string `json:"ctrl_l4"`
	CtrlDirect  string `json:"ctrl_direct"`
	CtrlWSS     string `json:"ctrl_wss"`
	CtrlMethods string `json:"ctrl_methods"`
}

type SharedRateLimiter struct {
	mu         sync.Mutex
	rateBps    float64
	tokens     float64
	lastUpdate time.Time
}

var (
	tunnelLimiters   = make(map[string]*SharedRateLimiter)
	tunnelLimitersMu sync.Mutex
)

func updateTunnelLimiter(tunID, speedLimitStr string) {
	mbps, _ := strconv.ParseFloat(strings.TrimSpace(speedLimitStr), 64)
	baseID := strings.TrimSuffix(tunID, "-exit")
	tunnelLimitersMu.Lock()
	defer tunnelLimitersMu.Unlock()
	if mbps <= 0 {
		delete(tunnelLimiters, tunID)
		delete(tunnelLimiters, baseID)
		return
	}
	bps := mbps * 1024 * 1024
	lim := &SharedRateLimiter{rateBps: bps, tokens: bps * 0.25, lastUpdate: time.Now()}
	tunnelLimiters[tunID] = lim
	tunnelLimiters[baseID] = lim
}

func getTunnelLimiter(tunID string) *SharedRateLimiter {
	tunnelLimitersMu.Lock()
	defer tunnelLimitersMu.Unlock()
	if lim, ok := tunnelLimiters[tunID]; ok {
		return lim
	}
	baseID := strings.TrimSuffix(tunID, "-exit")
	return tunnelLimiters[baseID]
}

func (l *SharedRateLimiter) WaitBytes(n int) {
	if l == nil || n <= 0 {
		return
	}
	l.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(l.lastUpdate).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * l.rateBps
		maxBurst := l.rateBps * 0.25
		if maxBurst < 65536 {
			maxBurst = 65536
		}
		if l.tokens > maxBurst {
			l.tokens = maxBurst
		}
		l.lastUpdate = now
	}
	l.tokens -= float64(n)
	var waitDur time.Duration
	if l.tokens < 0 {
		waitDur = time.Duration((-l.tokens / l.rateBps) * float64(time.Second))
	}
	l.mu.Unlock()
	if waitDur > 0 {
		time.Sleep(waitDur)
	}
}

func encryptSecret(plain string) string {
	if plain == "" || strings.HasPrefix(plain, "enc:") {
		return plain
	}
	key := sha256.Sum256([]byte("ghq-vault-" + localPinHex))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return plain
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain
	}
	nonce := make([]byte, gcm.NonceSize())
	_, _ = io.ReadFull(rand.Reader, nonce)
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(sealed)
}

func decryptSecret(val string) string {
	if !strings.HasPrefix(val, "enc:") {
		return val
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(val, "enc:"))
	if err != nil {
		return ""
	}
	key := sha256.Sum256([]byte("ghq-vault-" + localPinHex))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	ns := gcm.NonceSize()
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func syncServerTunnelsPinLocked(srvID, newPin string) {
	if newPin == "" {
		return
	}
	for i := range db.Tunnels {
		if db.Tunnels[i].TargetServer == srvID && db.Tunnels[i].Role != "server" {
			if db.Tunnels[i].CertPin != newPin {
				db.Tunnels[i].CertPin = newPin
				tCopy := db.Tunnels[i]
				if tCopy.SourceServer == "" || tCopy.SourceServer == "local" {
					go func(t Tunnel) {
						dispatchTunnelEngine(t)
						time.Sleep(400 * time.Millisecond)
						probeTunnelOnce(t)
					}(tCopy)
				}
			}
		}
	}
}

func getControlPorts() (l4, direct, wss string) {
	l4 = strings.TrimSpace(os.Getenv("CTRL_L4_PORT"))
	if l4 == "" {
		l4 = strings.TrimSpace(db.CtrlL4Port)
	}
	if l4 == "" {
		l4 = "6005"
	}
	direct = strings.TrimSpace(os.Getenv("CTRL_DIRECT_PORT"))
	if direct == "" {
		direct = strings.TrimSpace(db.CtrlDirectPort)
	}
	if direct == "" {
		direct = "6006"
	}
	wss = strings.TrimSpace(os.Getenv("CTRL_WSS_PORT"))
	if wss == "" {
		wss = strings.TrimSpace(db.CtrlWSSPort)
	}
	if wss == "" {
		wss = "6007"
	}
	return l4, direct, wss
}

func getAllowedControlMethods() []string {
	raw := strings.TrimSpace(os.Getenv("CTRL_METHODS"))
	if raw == "" {
		raw = strings.TrimSpace(db.CtrlMethods)
	}
	if raw == "" {
		raw = "wss,direct"
	}
	var list []string
	for _, m := range strings.Split(strings.ToLower(raw), ",") {
		m = strings.TrimSpace(m)
		if m == "wss" || m == "direct" || m == "forward" {
			list = append(list, m)
		}
	}
	if len(list) == 0 {
		return []string{"wss", "direct"}
	}
	return list
}

func executeLocalConsoleCmd(cmd, port string) {
	cmd = strings.TrimSpace(cmd)
	addLog("root@ghoghnoos:~# " + cmd)
	switch strings.ToLower(cmd) {
	case "help":
		addLog("Commands: status | bbr | pin | clear | restart")
	case "status":
		ram, goCnt := getMemoryStats()
		l4, dir, wss := getControlPorts()
		addLog(fmt.Sprintf("STATUS: %s | Goroutines: %d | RAM: %d MB | Panel: :%s | Ctrl: :%s/:%s/:%s", appVersion, goCnt, ram, port, l4, dir, wss))
	case "bbr":
		cc, qd := checkBBRStatus()
		addLog(fmt.Sprintf("BBR-CHECK: congestion_control=%s | default_qdisc=%s", cc, qd))
	case "pin":
		addLog("SHA256-PIN: " + localPinHex)
	case "clear":
		mu.Lock()
		logs = []string{"[SYSTEM] Terminal cleared."}
		_ = os.WriteFile(logPath, []byte(logs[0]+"\n"), 0644)
		mu.Unlock()
	case "restart":
		go func() { time.Sleep(500 * time.Millisecond); os.Exit(0) }()
	case "self-update":
		go triggerLocalBinaryUpdate()
	default:
		if strings.HasPrefix(strings.ToLower(cmd), "set-methods:") {
			mStr := strings.TrimPrefix(strings.ToLower(cmd), "set-methods:")
			_ = os.Setenv("CTRL_METHODS", mStr)
			mu.Lock()
			db.CtrlMethods = mStr
			saveDBLocked()
			mu.Unlock()
			addLog("CONTROL-METHODS: Updated allowed transports to [" + mStr + "]")
			return
		}
		if cmd != "" {
			addLog("bash: " + cmd + ": command not found")
		}
	}
}

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
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s", timestamp, msg)
	logs = append(logs, line)
	if len(logs) > 5000 {
		logs = logs[len(logs)-5000:]
	}
	_ = os.MkdirAll("/opt/ghoghnoos", 0755)
	if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		_, _ = f.WriteString(line + "\n")
		f.Close()
	}
}

func recordPingPoint(pt PingPoint) {
	mu.Lock()
	defer mu.Unlock()
	db.PingHistory = append(db.PingHistory, pt)
	if len(db.PingHistory) > 3000 {
		db.PingHistory = db.PingHistory[len(db.PingHistory)-3000:]
	}
	saveDBLocked()
}

func loadLogsFromDisk() {
	if data, err := os.ReadFile(logPath); err == nil {
		var valid []string
		for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.TrimSpace(l) != "" {
				valid = append(valid, l)
			}
		}
		if len(valid) > 5000 {
			valid = valid[len(valid)-5000:]
		}
		logs = valid
	}
	addLog("SYS: Ghoghnoos Tunnel initialized.")
}

func saveDBLocked() {
	_ = os.MkdirAll("/opt/ghoghnoos", 0755)
	dbCopy := db
	dbCopy.Servers = make([]ServerNode, len(db.Servers))
	for i, s := range db.Servers {
		s.SSHPass = encryptSecret(s.SSHPass)
		dbCopy.Servers[i] = s
	}
	data, _ := json.MarshalIndent(dbCopy, "", "  ")
	_ = os.WriteFile(dbPath, data, 0600)
}

func loadDBFromDisk(port string) {
	localIP = detectLocalIP()
	if data, err := os.ReadFile(dbPath); err == nil {
		_ = json.Unmarshal(data, &db)
	}
	for i := range db.Servers {
		db.Servers[i].SSHPass = decryptSecret(db.Servers[i].SSHPass)
	}
	if db.Prefs.Theme == "" {
		db.Prefs.Theme = "cyan"
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
	if db.CtrlL4Port == "" {
		db.CtrlL4Port = "6005"
	}
	if db.CtrlDirectPort == "" {
		db.CtrlDirectPort = "6006"
	}
	if db.CtrlWSSPort == "" {
		db.CtrlWSSPort = "6007"
	}
	if db.CtrlMethods == "" {
		db.CtrlMethods = "wss,direct"
	}

	hasLocal := false
	for i := range db.Servers {
		if db.Servers[i].ID == "local" {
			db.Servers[i].IP = localIP
			db.Servers[i].PanelPort = port
			db.Servers[i].CertPin = localPinHex
			db.Servers[i].Version = appVersion
			db.Servers[i].Status = "Online"
			db.Servers[i].IsLocal = true
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		master := ServerNode{
			ID: "local", Name: "Master", IP: localIP, SSHPort: "22",
			SSHUser: "root", PanelPort: port, CertPin: localPinHex, Version: appVersion, Status: "Online", IsLocal: true,
		}
		db.Servers = append([]ServerNode{master}, db.Servers...)
	}
	saveDBLocked()

	for _, t := range db.Tunnels {
		if t.SourceServer == "" || t.SourceServer == "local" || t.Role == "server" {
			dispatchTunnelEngine(t)
		}
	}
}

func dispatchTunnelEngine(t Tunnel) {
	updateTunnelLimiter(t.ID, t.SpeedLimit)
	switch t.Method {
	case "direct", "spoof":
		go startDirectTLSEngine(t)
	case "forward":
		go startPortForwardEngine(t)
	case "grpc":
		go startGRPCEngine(t)
	default:
		go startWSSEngine(t)
	}
}

func findServerByID(id string) *ServerNode {
	for i := range db.Servers {
		if db.Servers[i].ID == id {
			return &db.Servers[i]
		}
	}
	return nil
}

func updateConfigFile(req ConfigReq) error {
	data, _ := os.ReadFile(confPath)
	kv := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if parts := strings.SplitN(strings.TrimSpace(line), "=", 2); len(parts) == 2 {
			kv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	if req.Port != "" {
		kv["PANEL_PORT"] = req.Port
	}
	kv["PANEL_PATH"] = strings.Trim(req.Path, "/")
	if req.User != "" {
		kv["PANEL_USER"] = req.User
	}
	if req.Pass != "" {
		kv["PANEL_PASS"] = req.Pass
	}
	if req.CtrlL4 != "" {
		kv["CTRL_L4_PORT"] = req.CtrlL4
	}
	if req.CtrlDirect != "" {
		kv["CTRL_DIRECT_PORT"] = req.CtrlDirect
	}
	if req.CtrlWSS != "" {
		kv["CTRL_WSS_PORT"] = req.CtrlWSS
	}
	if req.CtrlMethods != "" {
		kv["CTRL_METHODS"] = req.CtrlMethods
	}
	var out []string
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	_ = os.MkdirAll("/opt/ghoghnoos", 0755)
	return os.WriteFile(confPath, []byte(strings.Join(out, "\n")+"\n"), 0644)
}

func getMemoryStats() (uint64, int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024, runtime.NumGoroutine()
}
