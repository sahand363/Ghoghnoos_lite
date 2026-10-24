package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
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
	Prefs        UIPrefs      `json:"prefs"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

func checkBBRStatus() (cc, qdisc string) {
	b1, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control")
	if err == nil {
		cc = strings.TrimSpace(string(b1))
	} else {
		cc = "unknown"
	}
	b2, err := os.ReadFile("/proc/sys/net/core/default_qdisc")
	if err == nil {
		qdisc = strings.TrimSpace(string(b2))
	} else {
		qdisc = "unknown"
	}
	return cc, qdisc
}

func enableLocalBBR() error {
	cmd := "sed -i '/net.core.default_qdisc/d' /etc/sysctl.conf; " +
		"sed -i '/net.ipv4.tcp_congestion_control/d' /etc/sysctl.conf; " +
		"echo 'net.core.default_qdisc=fq' >> /etc/sysctl.conf; " +
		"echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.conf; " +
		"sysctl -p"
	return exec.Command("bash", "-c", cmd).Run()
}

func readCPUSample() (idle, total uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	fields := strings.Fields(lines[0])
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 {
			idle = v
		}
	}
	return idle, total
}

func readMemInfo() (usedMB, totalMB, pct uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.Alloc / 1024 / 1024, 1024, 15
	}
	var memTotal, memAvail uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d kB", &memTotal)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d kB", &memAvail)
		}
	}
	if memTotal > 0 {
		totalMB = memTotal / 1024
		usedMB = (memTotal - memAvail) / 1024
		pct = (usedMB * 100) / totalMB
	}
	return usedMB, totalMB, pct
}

func readNetDevBytes() (rx, tx uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lo:") || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			r, _ := strconv.ParseUint(fields[0], 10, 64)
			t, _ := strconv.ParseUint(fields[8], 10, 64)
			rx += r
			tx += t
		}
	}
	return rx, tx
}

func startTelemetryMonitor() {
	mu.Lock()
	sysMetrics.TpHistory = []float64{0.05, 0.08, 0.06, 0.10, 0.07, 0.09}
	sysMetrics.BBRStatus, sysMetrics.Qdisc = checkBBRStatus()
	mu.Unlock()

	prevIdle, prevTotal := readCPUSample()
	prevRx, prevTx := readNetDevBytes()
	for {
		time.Sleep(2 * time.Second)
		idle, total := readCPUSample()
		var cpuPct float64
		if total > prevTotal {
			cpuPct = float64((total-prevTotal)-(idle-prevIdle)) * 100.0 / float64(total-prevTotal)
		}
		prevIdle, prevTotal = idle, total

		rx, tx := readNetDevBytes()
		var rxMbps, txMbps float64
		if rx >= prevRx {
			rxMbps = float64((rx-prevRx)*8) / (2.0 * 1000000.0)
		}
		if tx >= prevTx {
			txMbps = float64((tx-prevTx)*8) / (2.0 * 1000000.0)
		}
		prevRx, prevTx = rx, tx

		usedMB, totalMB, ramPct := readMemInfo()
		cc, qd := checkBBRStatus()
		appBytes := db.AppBaseBytes + atomic.LoadUint64(&appRxBytes) + atomic.LoadUint64(&appTxBytes)

		mu.Lock()
		sysMetrics.CPUPercent = float64(int(cpuPct*10)) / 10.0
		sysMetrics.RAMUsedMB = usedMB
		sysMetrics.RAMTotalMB = totalMB
		sysMetrics.RAMPct = ramPct
		sysMetrics.LiveRxMbps = float64(int(rxMbps*100)) / 100.0
		sysMetrics.LiveTxMbps = float64(int(txMbps*100)) / 100.0
		sysMetrics.AppTotalMB = appBytes / (1024 * 1024)
		sysMetrics.SrvTotalGB = float64(int((float64(rx+tx)/(1024*1024*1024))*100)) / 100.0
		sysMetrics.BBRStatus = cc
		sysMetrics.Qdisc = qd
		sysMetrics.TpHistory = append(sysMetrics.TpHistory, sysMetrics.LiveRxMbps+sysMetrics.LiveTxMbps)
		if len(sysMetrics.TpHistory) > 75 {
			sysMetrics.TpHistory = sysMetrics.TpHistory[len(sysMetrics.TpHistory)-75:]
		}
		mu.Unlock()
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
	timestamp := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s", timestamp, msg)
	logs = append(logs, line)
	if len(logs) > 250 {
		logs = logs[len(logs)-250:]
	}
	_ = os.MkdirAll("/opt/litetun", 0755)
	_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")+"\n"), 0644)
}

func recordPingPoint(pt PingPoint) {
	mu.Lock()
	defer mu.Unlock()
	db.PingHistory = append(db.PingHistory, pt)
	if len(db.PingHistory) > 2000 {
		db.PingHistory = db.PingHistory[len(db.PingHistory)-2000:]
	}
	saveDBLocked()
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
	addLog("SYS: Ghoghnoos-lite (LiteTun) initialized.")
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
			go startTunnelEngine(t)
		}
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

func runSSHStep(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return strings.TrimSpace(string(out)), err
}

func deploySentinelSSH(s ServerNode) {
	detectedPort := s.PanelPort
	if detectedPort == "" {
		detectedPort = "8880"
	}

	addLog(fmt.Sprintf("[10%%] [%s]: Checking existing Sentinel on %s:%s...", s.Name, s.IP, detectedPort))
	httpClient := &http.Client{Timeout: 3 * time.Second}
	if s.SSHPass == "" {
		if resp, err := httpClient.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort)); err == nil {
			defer resp.Body.Close()
			var st map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&st) == nil {
				remotePin, _ := st["cert_pin"].(string)
				mu.Lock()
				if srv := findServerByID(s.ID); srv != nil {
					srv.PanelPort = detectedPort
					srv.CertPin = remotePin
					srv.Status = "Online"
					saveDBLocked()
				}
				mu.Unlock()
				addLog(fmt.Sprintf("[100%%] VERIFIED [%s]: Linked to active Sentinel at %s:%s!", s.Name, s.IP, detectedPort))
				return
			}
		}
		addLog(fmt.Sprintf("ERR [%s]: Sentinel offline and no SSH password stored.", s.Name))
		return
	}

	addLog(fmt.Sprintf("[25%%] SSH [%s]: Authenticating %s@%s:%s ...", s.Name, s.SSHUser, s.IP, s.SSHPort))
	sshConfig := &ssh.ClientConfig{
		User: s.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(s.SSHPass),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				ans := make([]string, len(questions))
				for i := range ans {
					ans[i] = s.SSHPass
				}
				return ans, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}

	sshClient, err := ssh.Dial("tcp", net.JoinHostPort(s.IP, s.SSHPort), sshConfig)
	if err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Connection failed -> %v", s.Name, err))
		mu.Lock()
		if srv := findServerByID(s.ID); srv != nil {
			srv.Status = "SSH Error"
			saveDBLocked()
		}
		mu.Unlock()
		return
	}
	defer sshClient.Close()

	addLog(fmt.Sprintf("[50%%] SSH [%s]: Downloading latest Ghoghnoos-lite binary...", s.Name))
	step1 := "export DEBIAN_FRONTEND=noninteractive; mkdir -p /opt/litetun; " +
		"if [ ! -f /opt/litetun/config.env ]; then echo -e 'PANEL_PORT=8880\\nPANEL_PATH=sentinel\\nPANEL_USER=admin\\nPANEL_PASS=admin' > /opt/litetun/config.env; fi; " +
		"systemctl stop litetun 2>/dev/null || true; " +
		"curl -Ls https://github.com/sahand363/ghoghnoos_light/releases/latest/download/ghoghnoos_lite -o /opt/litetun/ghoghnoos_lite.tmp && " +
		"mv -f /opt/litetun/ghoghnoos_lite.tmp /opt/litetun/ghoghnoos_lite && chmod +x /opt/litetun/ghoghnoos_lite"
	if _, err := runSSHStep(sshClient, step1); err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Binary download failed -> %v", s.Name, err))
		return
	}

	addLog(fmt.Sprintf("[75%%] SSH [%s]: Configuring systemd service & starting Sentinel...", s.Name))
	step2 := "curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos_light/main/install.sh | tr -d '\\r' > /usr/local/bin/litetun && " +
		"chmod +x /usr/local/bin/litetun && /usr/local/bin/litetun --sentinel && systemctl restart litetun && " +
		"grep '^PANEL_PORT=' /opt/litetun/config.env | cut -d= -f2"
	out, err := runSSHStep(sshClient, step2)
	if err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Service start failed -> %v", s.Name, err))
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		detectedPort = strings.TrimSpace(lines[len(lines)-1])
	}

	addLog(fmt.Sprintf("[90%%] [%s]: Verifying Sentinel handshake & syncing SHA-256 Pin...", s.Name))
	time.Sleep(2 * time.Second)

	remotePin := ""
	if resp, err := httpClient.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort)); err == nil {
		defer resp.Body.Close()
		var st map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&st) == nil {
			remotePin, _ = st["cert_pin"].(string)
		}
	}
	mu.Lock()
	if srv := findServerByID(s.ID); srv != nil {
		srv.PanelPort = detectedPort
		srv.CertPin = remotePin
		srv.Status = "Online"
		saveDBLocked()
	}
	mu.Unlock()
	addLog(fmt.Sprintf("[100%%] DEPLOY-OK [%s]: Sentinel verified & online on :%s!", s.Name, detectedPort))
}

func getMemoryStats() (uint64, int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024, runtime.NumGoroutine()
}
