package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
	Status    string `json:"status"`
	ViaMethod string `json:"via_method,omitempty"`
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
}

type UIPrefs struct {
	Theme          string `json:"theme"`
	Lang           string `json:"lang"`
	ActiveTab      string `json:"active_tab"`
	SelectedServer string `json:"selected_server"`
}

type Database struct {
	Servers      []ServerNode `json:"servers"`
	Tunnels      []Tunnel     `json:"tunnels"`
	PingHistory  []PingPoint  `json:"ping_history"`
	AppBaseBytes uint64       `json:"app_base_bytes"`
	EnrollToken  string       `json:"enroll_token"`
	Prefs        UIPrefs      `json:"prefs"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

func executeLocalConsoleCmd(cmd, port string) {
	cmd = strings.TrimSpace(cmd)
	addLog("root@litetun:~# " + cmd)
	switch strings.ToLower(cmd) {
	case "help":
		addLog("Commands: status | bbr | pin | clear | restart")
	case "status":
		ram, goCnt := getMemoryStats()
		addLog(fmt.Sprintf("STATUS: Online | Goroutines: %d | RAM: %d MB | Port: %s", goCnt, ram, port))
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
	default:
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

	_ = os.MkdirAll("/opt/litetun", 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
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
	data, err := os.ReadFile(logPath)
	if err == nil {
		rawLines := strings.Split(strings.TrimSpace(string(data)), "\n")
		var valid []string
		for _, l := range rawLines {
			if strings.TrimSpace(l) != "" {
				valid = append(valid, l)
			}
		}
		if len(valid) > 5000 {
			valid = valid[len(valid)-5000:]
		}
		logs = valid
	}
	addLog("SYS: Ghoghnoos-lite initialized.")
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
			ID: "local", Name: "Master", IP: localIP, SSHPort: "22",
			SSHUser: "root", PanelPort: port, CertPin: localPinHex, Status: "Online", IsLocal: true,
		}
		db.Servers = append([]ServerNode{master}, db.Servers...)
	}
	saveDBLocked()

	for _, t := range db.Tunnels {
		if t.SourceServer == "" || t.SourceServer == "local" {
			dispatchTunnelEngine(t)
		}
	}
}

func dispatchTunnelEngine(t Tunnel) {
	switch t.Method {
	case "direct", "spoof":
		go startDirectTLSEngine(t)
	case "forward":
		go startPortForwardEngine(t)
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

func updateConfigFile(newPort, newPath, newUser, newPass string) error {
	data, _ := os.ReadFile(confPath)
	lines := strings.Split(string(data), "\n")
	hasPort, hasPath, hasUser, hasPass := false, false, false, false

	for i, line := range lines {
		if strings.HasPrefix(line, "PANEL_PORT=") && newPort != "" {
			lines[i] = "PANEL_PORT=" + newPort
			hasPort = true
		} else if strings.HasPrefix(line, "PANEL_PATH=") {
			lines[i] = "PANEL_PATH=" + strings.Trim(newPath, "/")
			hasPath = true
		} else if strings.HasPrefix(line, "PANEL_USER=") && newUser != "" {
			lines[i] = "PANEL_USER=" + newUser
			hasUser = true
		} else if strings.HasPrefix(line, "PANEL_PASS=") && newPass != "" {
			lines[i] = "PANEL_PASS=" + newPass
			hasPass = true
		}
	}
	if !hasPort && newPort != "" {
		lines = append(lines, "PANEL_PORT="+newPort)
	}
	if !hasPath {
		lines = append(lines, "PANEL_PATH="+strings.Trim(newPath, "/"))
	}
	if !hasUser && newUser != "" {
		lines = append(lines, "PANEL_USER="+newUser)
	}
	if !hasPass && newPass != "" {
		lines = append(lines, "PANEL_PASS="+newPass)
	}
	_ = os.MkdirAll("/opt/litetun", 0755)
	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
}

func getMemoryStats() (uint64, int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024, runtime.NumGoroutine()
}
