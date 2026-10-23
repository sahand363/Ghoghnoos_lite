package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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
	IsLocal   bool   `json:"is_local"`
}

type Tunnel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Method       string `json:"method"`
	SourceServer string `json:"source_server"`
	TargetServer string `json:"target_server"`
	ListenPort   string `json:"listen_port"`
	WSSPort      string `json:"wss_port"`
	TargetAddr   string `json:"target_addr"`
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
		addLog(fmt.Sprintf("SSH-ERR [%s]: Failed -> %v (%s)", s.Name, err, strings.TrimSpace(string(out))))
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

	addLog(fmt.Sprintf("SENTINEL [%s]: Deployed on %s:%s! Syncing...", s.Name, s.IP, detectedPort))
	time.Sleep(2 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort))
	remotePin := ""
	if err == nil {
		defer resp.Body.Close()
		var st map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&st) == nil {
			if p, ok := st["cert_pin"].(string); ok {
				remotePin = p
			}
		}
		addLog(fmt.Sprintf("CONNECTED [%s]: Sentinel Online! SHA-256 Pin synced.", s.Name))
	}

	mu.Lock()
	if srv := findServerByID(s.ID); srv != nil {
		srv.PanelPort = detectedPort
		srv.CertPin = remotePin
		srv.Status = "Online"
		saveDBLocked()
	}
	mu.Unlock()
}
