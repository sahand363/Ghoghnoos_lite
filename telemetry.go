package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
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
	tickCount := 0
	var accumTp float64

	for {
		time.Sleep(2 * time.Second)
		tickCount++
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

		curTp := sysMetrics.LiveRxMbps + sysMetrics.LiveTxMbps
		accumTp += curTp
		// Record 10-second average points up to 180 points (30 minutes of history)
		if len(sysMetrics.TpHistory) < 15 {
			sysMetrics.TpHistory = append(sysMetrics.TpHistory, curTp)
		} else if tickCount%5 == 0 {
			avgTp := float64(int((accumTp/5.0)*100)) / 100.0
			accumTp = 0
			sysMetrics.TpHistory = append(sysMetrics.TpHistory, avgTp)
			if len(sysMetrics.TpHistory) > 180 {
				sysMetrics.TpHistory = sysMetrics.TpHistory[len(sysMetrics.TpHistory)-180:]
			}
		}
		mu.Unlock()
	}
}

func uninstallSentinelRemote(s ServerNode) {
	pPort := s.PanelPort
	if pPort == "" {
		pPort = "6001"
	}
	mu.Lock()
	tok := getServerAuthToken(&s)
	mu.Unlock()

	addLog(fmt.Sprintf("UNINSTALL [%s]: Sending remote uninstall signal to %s...", s.Name, s.IP))
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/api/uninstall", s.IP, pPort), nil)
	req.Header.Set("X-Ghq-Token", tok)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			addLog(fmt.Sprintf("UNINSTALL-OK [%s]: Sentinel self-destruct triggered on %s!", s.Name, s.IP))
			return
		}
	}

	l4, dir, wss, grpcP := getControlPorts()
	targetSpec := fmt.Sprintf("%s:%s:%s:%s:%s", s.IP, l4, dir, wss, grpcP)
	for _, cand := range getAllowedControlMethods() {
		if rep, _, err := tryDialControl(targetSpec, tok, "uninstall", cand, true, nil); err == nil && rep.OK {
			addLog(fmt.Sprintf("UNINSTALL-OK [%s]: Sentinel self-destruct triggered via %s!", s.Name, strings.ToUpper(cand)))
			return
		}
	}
	addLog(fmt.Sprintf("UNINSTALL-WARN [%s]: Removed from panel (remote host unreachable).", s.Name))
}
