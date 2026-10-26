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
	Servers        []ServerNode `json:"servers"`
	Tunnels        []Tunnel     `json:"tunnels"`
	PingHistory    []PingPoint  `json:"ping_history"`
	AppBaseBytes   uint64       `json:"app_base_bytes"`
	EnrollToken    string       `json:"enroll_token"`
	CtrlL4Port     string       `json:"ctrl_l4_port"`
	CtrlDirectPort string       `json:"ctrl_direct_port"`
	CtrlWSSPort    string       `json:"ctrl_wss_port"`
	Prefs          UIPrefs      `json:"prefs"`
}

type ConfigReq struct {
	Port       string `json:"port"`
	Path       string `json:"path"`
	User       string `json:"user"`
	Pass       string `json:"pass"`
	CtrlL4     string `json:"ctrl_l4"`
	CtrlDirect string `json:"ctrl_direct"`
	CtrlWSS    string `json:"ctrl_wss"`
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

func executeLocalConsoleCmd(cmd, port string) {
	cmd = strings.TrimSpace(cmd)
	addLog("root@litetun:~# " + cmd)
	switch strings.ToLower(cmd) {
	case "help":
		addLog("Commands: status | bbr | pin | clear | restart")
	case "status":
		ram, goCnt := getMemoryStats()
		l4, dir, wss := getControlPorts()
		addLog(fmt.Sprintf("STATUS: Online | Goroutines: %d | RAM: %d MB | Panel: :%s | Ctrl: :%s/:%s/:%s", goCnt, ram, port, l4, dir, wss))
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
	addLog("SYS: Ghoghnoos-lite initialized.")
}

func saveDBLocked() {
	_ = os.MkdirAll("/opt/litetun", 0755)
	data, _ := json.MarshalIndent(db, "", "  ")
	_ = os.WriteFile(dbPath, data, 0644)
}

func loadDBFromDisk(port string) {
	localIP = detectLocalIP()
	if data, err := os.ReadFile(dbPath); err == nil {
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
	if db.CtrlL4Port == "" {
		db.CtrlL4Port = "6005"
	}
	if db.CtrlDirectPort == "" {
		db.CtrlDirectPort = "6006"
	}
	if db.CtrlWSSPort == "" {
		db.CtrlWSSPort = "6007"
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
	var out []string
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	_ = os.MkdirAll("/opt/litetun", 0755)
	return os.WriteFile(confPath, []byte(strings.Join(out, "\n")+"\n"), 0644)
}

func getMemoryStats() (uint64, int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024, runtime.NumGoroutine()
}
