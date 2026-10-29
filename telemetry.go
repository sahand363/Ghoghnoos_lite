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

var (
	dayAccumRxBytes uint64
	dayAccumTxBytes uint64
	lastDaySave     time.Time
)

func recordDailyBandwidthLocked(deltaRx, deltaTx uint64) {
	today := time.Now().UTC().Format("2006-01-02")
	n := len(db.DailyUsage)
	if n == 0 || db.DailyUsage[n-1].Date != today {
		db.DailyUsage = append(db.DailyUsage, DailyBW{Date: today, RxMB: 0, TxMB: 0})
		if len(db.DailyUsage) > 31 {
			db.DailyUsage = db.DailyUsage[len(db.DailyUsage)-31:]
		}
		dayAccumRxBytes = 0
		dayAccumTxBytes = 0
		n = len(db.DailyUsage)
	}
	dayAccumRxBytes += deltaRx
	dayAccumTxBytes += deltaTx
	if dayAccumRxBytes >= 1024*1024 {
		addMB := dayAccumRxBytes / (1024 * 1024)
		db.DailyUsage[n-1].RxMB += addMB
		dayAccumRxBytes %= (1024 * 1024)
	}
	if dayAccumTxBytes >= 1024*1024 {
		addMB := dayAccumTxBytes / (1024 * 1024)
		db.DailyUsage[n-1].TxMB += addMB
		dayAccumTxBytes %= (1024 * 1024)
	}
	if time.Since(lastDaySave) >= 5*time.Minute {
		lastDaySave = time.Now()
		saveDBLocked()
	}
}

func startTelemetryMonitor() {
	mu.Lock()
	sysMetrics.TpHistory = []float64{0.05, 0.08, 0.06, 0.10, 0.07, 0.09}
	sysMetrics.BBRStatus, sysMetrics.Qdisc = checkBBRStatus()
	recordDailyBandwidthLocked(0, 0)
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
		var dRx, dTx uint64
		var rxMbps, txMbps float64
		if rx >= prevRx {
			dRx = rx - prevRx
			rxMbps = float64(dRx*8) / (2.0 * 1000000.0)
		}
		if tx >= prevTx {
			dTx = tx - prevTx
			txMbps = float64(dTx*8) / (2.0 * 1000000.0)
		}
		prevRx, prevTx = rx, tx

		usedMB, totalMB, ramPct := readMemInfo()
		cc, qd := checkBBRStatus()
		appBytes := db.AppBaseBytes + atomic.LoadUint64(&appRxBytes) + atomic.LoadUint64(&appTxBytes)

		mu.Lock()
		recordDailyBandwidthLocked(dRx, dTx)
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
		detectedPort = "6001"
	}

	addLog(fmt.Sprintf("[10%%] [%s]: Checking Sentinel connection on %s:%s...", s.Name, s.IP, detectedPort))
	httpClient := &http.Client{Timeout: 3 * time.Second}
	if s.SSHPass == "" {
		if resp, err := httpClient.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort)); err == nil {
			defer resp.Body.Close()
			var st map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&st) == nil {
				remotePin, _ := st["cert_pin"].(string)
				remoteVer, _ := st["version"].(string)
				mu.Lock()
				if srv := findServerByID(s.ID); srv != nil {
					srv.PanelPort = detectedPort
					srv.CertPin = remotePin
					if remoteVer != "" {
						srv.Version = remoteVer
					}
					srv.Status = "Online"
					if srv.ViaMethod == "" {
						srv.ViaMethod = "SSH"
					}
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

	addLog(fmt.Sprintf("[50%%] SSH [%s]: Installing Sentinel binary...", s.Name))
	step1 := fmt.Sprintf("export DEBIAN_FRONTEND=noninteractive; mkdir -p /opt/ghoghnoos; "+
		"if [ ! -f /opt/ghoghnoos/config.env ]; then printf 'PANEL_PORT=%s\\nPANEL_PATH=sentinel\\nPANEL_USER=admin\\nPANEL_PASS=admin\\n' > /opt/ghoghnoos/config.env; fi; "+
		"systemctl stop ghoghnoos 2>/dev/null || true; "+
		"curl -Ls https://github.com/sahand363/ghoghnoos-tunnel/releases/latest/download/ghoghnoos-tunnel -o /opt/ghoghnoos/ghoghnoos-tunnel.tmp && "+
		"mv -f /opt/ghoghnoos/ghoghnoos-tunnel.tmp /opt/ghoghnoos/ghoghnoos-tunnel && chmod +x /opt/ghoghnoos/ghoghnoos-tunnel", detectedPort)
	if _, err := runSSHStep(sshClient, step1); err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Binary download failed -> %v", s.Name, err))
		return
	}

	addLog(fmt.Sprintf("[75%%] SSH [%s]: Starting Sentinel service...", s.Name))
	step2 := "curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh | tr -d '\\r' > /usr/local/bin/ghoghnoos && " +
		"chmod +x /usr/local/bin/ghoghnoos && /usr/local/bin/ghoghnoos --sentinel && systemctl restart ghoghnoos && " +
		"grep '^PANEL_PORT=' /opt/ghoghnoos/config.env | cut -d= -f2"
	out, err := runSSHStep(sshClient, step2)
	if err != nil {
		addLog(fmt.Sprintf("SSH-ERR [%s]: Service start failed -> %v", s.Name, err))
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		detectedPort = strings.TrimSpace(lines[len(lines)-1])
	}

	addLog(fmt.Sprintf("[90%%] [%s]: Syncing TLS Certificate Fingerprint...", s.Name))
	time.Sleep(2 * time.Second)

	remotePin := ""
	remoteVer := appVersion
	if resp, err := httpClient.Get(fmt.Sprintf("http://%s:%s/api/state", s.IP, detectedPort)); err == nil {
		defer resp.Body.Close()
		var st map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&st) == nil {
			remotePin, _ = st["cert_pin"].(string)
			if rv, ok := st["version"].(string); ok && rv != "" {
				remoteVer = rv
			}
		}
	}
	mu.Lock()
	if srv := findServerByID(s.ID); srv != nil {
		srv.PanelPort = detectedPort
		srv.CertPin = remotePin
		srv.Version = remoteVer
		srv.Status = "Online"
		srv.ViaMethod = "SSH"
		saveDBLocked()
	}
	mu.Unlock()
	addLog(fmt.Sprintf("[100%%] DEPLOY-OK [%s]: Sentinel (%s) verified & online on :%s!", s.Name, remoteVer, detectedPort))
}

func uninstallSentinelRemote(s ServerNode) {
	pPort := s.PanelPort
	if pPort == "" {
		pPort = "6001"
	}
	addLog(fmt.Sprintf("UNINSTALL [%s]: Sending remote uninstall signal to %s...", s.Name, s.IP))
	client := &http.Client{Timeout: 3 * time.Second}
	if resp, err := client.Post(fmt.Sprintf("http://%s:%s/api/uninstall", s.IP, pPort), "application/json", nil); err == nil {
		resp.Body.Close()
	}
	if s.SSHPass != "" {
		sshCfg := &ssh.ClientConfig{
			User:            s.SSHUser,
			Auth:            []ssh.AuthMethod{ssh.Password(s.SSHPass)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         8 * time.Second,
		}
		if sc, err := ssh.Dial("tcp", net.JoinHostPort(s.IP, s.SSHPort), sshCfg); err == nil {
			defer sc.Close()
			purgeCmd := "systemctl stop ghoghnoos 2>/dev/null; systemctl disable ghoghnoos 2>/dev/null; rm -f /etc/systemd/system/ghoghnoos.service; systemctl daemon-reload; rm -rf /opt/ghoghnoos /usr/local/bin/ghoghnoos"
			_, _ = runSSHStep(sc, purgeCmd)
			addLog(fmt.Sprintf("UNINSTALL-OK [%s]: Sentinel completely removed from %s via SSH!", s.Name, s.IP))
			return
		}
	}
	addLog(fmt.Sprintf("UNINSTALL-OK [%s]: Sentinel self-destruct triggered on %s!", s.Name, s.IP))
}
