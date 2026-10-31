package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func buildPingSummary(all []PingPoint, targetSrvID, rangeStr string) map[string]interface{} {
	nowSec := time.Now().Unix()
	rangeSec := int64(86400)
	if rangeStr == "7d" {
		rangeSec = 7 * 86400
	}

	var srvHist []PingPoint
	for _, p := range all {
		if targetSrvID != "" && targetSrvID != "local" {
			if p.ServerID == targetSrvID {
				srvHist = append(srvHist, p)
			}
		} else {
			if p.ServerID == "" || p.ServerID == "local" {
				srvHist = append(srvHist, p)
			}
		}
	}
	// If Master ("local") is selected and all tunnels target remote agents, show Master's outbound tunnel pings
	if (targetSrvID == "" || targetSrvID == "local") && len(srvHist) == 0 {
		srvHist = all
	}

	last20 := srvHist
	if len(last20) > 20 {
		last20 = last20[len(last20)-20:]
	}
	rOk := 0
	for _, x := range last20 {
		if x.OK {
			rOk++
		}
	}
	reachStr := "20 / 20"
	if len(last20) > 0 {
		reachStr = fmt.Sprintf("%d / %d", rOk, len(last20))
	}

	var filtered []PingPoint
	var okSum int64
	var okCnt, fails int
	var latestMs int64 = 50
	var recentPings []int64
	var windows []string
	var curStart int64

	for _, p := range srvHist {
		if p.Timestamp >= nowSec-rangeSec {
			filtered = append(filtered, p)
			if p.OK && p.LatencyMS > 0 {
				okSum += p.LatencyMS
				okCnt++
				latestMs = p.LatencyMS
				recentPings = append(recentPings, p.LatencyMS)
				if curStart != 0 {
					windows = append(windows, fmt.Sprintf("%s ➔ %s", time.Unix(curStart, 0).Format("01-02 15:04"), time.Unix(p.Timestamp, 0).Format("15:04")))
					curStart = 0
				}
			} else if !p.OK {
				fails++
				if curStart == 0 {
					curStart = p.Timestamp
				}
			}
		}
	}
	if curStart != 0 {
		windows = append(windows, fmt.Sprintf("%s ➔ NOW", time.Unix(curStart, 0).Format("01-02 15:04")))
	}
	if len(recentPings) > 25 {
		recentPings = recentPings[len(recentPings)-25:]
	}
	if len(windows) > 4 {
		windows = windows[len(windows)-4:]
	}
	avgMs := int64(50)
	if okCnt > 0 {
		avgMs = okSum / int64(okCnt)
	}
	uptimePct := "100.0"
	if len(filtered) > 0 {
		uptimePct = fmt.Sprintf("%.1f", float64(okCnt)*100.0/float64(len(filtered)))
	}

	// Evenly downsample across the entire 24h / 7d window while preserving any offline failure points
	var chartPts []PingPoint
	maxPts := 120
	if len(filtered) <= maxPts {
		chartPts = filtered
	} else {
		step := float64(len(filtered)-1) / float64(maxPts-1)
		for i := 0; i < maxPts; i++ {
			idx := int(float64(i) * step)
			pt := filtered[idx]
			// Check if any point in this bucket failed so outages are never hidden by downsampling
			nextIdx := int(float64(i+1) * step)
			if nextIdx > len(filtered) {
				nextIdx = len(filtered)
			}
			for j := idx; j < nextIdx; j++ {
				if !filtered[j].OK {
					pt = filtered[j]
					break
				}
			}
			chartPts = append(chartPts, pt)
		}
	}

	return map[string]interface{}{
		"reach_str":    reachStr,
		"uptime_pct":   uptimePct,
		"avg_ms":       avgMs,
		"latest_ms":    latestMs,
		"fails":        fails,
		"recent_pings": recentPings,
		"windows":      windows,
		"chart_points": chartPts,
	}
}

func pushExitTunnelToRemote(dstSrv ServerNode, exitTunnel Tunnel, tunName string) string {
	mu.Lock()
	tok := getServerAuthToken(&dstSrv)
	var allExits []Tunnel
	hasThis := false
	for _, t := range db.Tunnels {
		if t.TargetServer == dstSrv.ID && t.Role != "server" {
			ex := Tunnel{
				ID: t.ID + "-exit", Name: t.Name + " (Exit)", Role: "server", Method: t.Method,
				TunnelPort: t.TunnelPort, InboundPorts: t.InboundPorts, SNI: t.SNI,
				Fingerprint: t.Fingerprint, FragSize: t.FragSize, SpeedLimit: t.SpeedLimit, WSPath: t.WSPath, Status: "Online",
			}
			if ex.ID == exitTunnel.ID {
				ex = exitTunnel
				hasThis = true
			}
			allExits = append(allExits, ex)
		}
	}
	if !hasThis {
		allExits = append(allExits, exitTunnel)
	}
	mu.Unlock()

	pPort := dstSrv.PanelPort
	if pPort == "" {
		pPort = "6001"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	body, _ := json.Marshal(exitTunnel)
	if reqMsg, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/api/tunnel", dstSrv.IP, pPort), bytes.NewReader(body)); err == nil {
		reqMsg.Header.Set("Content-Type", "application/json")
		reqMsg.Header.Set("X-Ghq-Token", tok)
		if resp, err := client.Do(reqMsg); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				addLog(fmt.Sprintf("EXIT-OK [%s]: %s :%s -> 127.0.0.1:[%s] on %s", tunName, strings.ToUpper(exitTunnel.Method), exitTunnel.TunnelPort, exitTunnel.InboundPorts, dstSrv.Name))
				return dstSrv.CertPin
			}
		}
	}

	l4, dir, wss, grpcP := getControlPorts()
	targetSpec := fmt.Sprintf("%s:%s:%s:%s:%s", dstSrv.IP, l4, dir, wss, grpcP)
	for _, cand := range getAllowedControlMethods() {
		if rep, _, err := tryDialControl(targetSpec, tok, "", cand, true, allExits); err == nil && rep.OK {
			applyForwardProbeSuccess(dstSrv.ID, cand, tok, rep)
			addLog(fmt.Sprintf("EXIT-OK [%s]: %s :%s -> 127.0.0.1:[%s] on %s (via %s)", tunName, strings.ToUpper(exitTunnel.Method), exitTunnel.TunnelPort, exitTunnel.InboundPorts, dstSrv.Name, strings.ToUpper(cand)))
			return rep.MasterPin
		}
	}

	addLog(fmt.Sprintf("EXIT-SYNC [%s]: Queued Exit listener for %s via Control Link", tunName, dstSrv.Name))
	return dstSrv.CertPin
}

func registerRoutes(port, panelPath string) {
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		handleLoginRoute(w, r, panelPath)
	})
	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		handleLogoutRoute(w, r, panelPath)
	})
	if panelPath != "" {
		http.HandleFunc("/"+panelPath+"/login", func(w http.ResponseWriter, r *http.Request) {
			handleLoginRoute(w, r, panelPath)
		})
		http.HandleFunc("/"+panelPath+"/logout", func(w http.ResponseWriter, r *http.Request) {
			handleLogoutRoute(w, r, panelPath)
		})
	}

	http.HandleFunc("/api/update", func(w http.ResponseWriter, r *http.Request) {
		handleSelfUpdateRoute(w, r, panelPath)
	})

	http.HandleFunc("/i18n.js", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(i18nJS)
	})

	http.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(appJS)
	})

	http.HandleFunc("/api/logs/download", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			tok := getServerAuthToken(srv)
			mu.Unlock()
			if srv != nil && srv.IP != "" {
				client := &http.Client{Timeout: 5 * time.Second}
				req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%s/api/logs/download", srv.IP, srv.PanelPort), nil)
				req.Header.Set("X-Ghq-Token", tok)
				if resp, err := client.Do(req); err == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						w.Header().Set("Content-Type", "text/plain; charset=utf-8")
						w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ghoghnoos_%s_%s.log\"", srv.Name, time.Now().Format("20060102_1504")))
						io.Copy(w, resp.Body)
						return
					}
				}
			}
		}
		data, err := os.ReadFile(logPath)
		if err != nil {
			mu.Lock()
			data = []byte(strings.Join(logs, "\n") + "\n")
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ghoghnoos_master_%s.log\"", time.Now().Format("20060102_1504")))
		w.Write(data)
	})

	http.HandleFunc("/api/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var hb EnrollHeartbeat
		if json.NewDecoder(r.Body).Decode(&hb) != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		rep := processIncomingEnroll(hb)
		if !rep.OK {
			http.Error(w, "Invalid enrollment token", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rep)
	})

	http.HandleFunc("/api/enroll/token", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		newTok := regenerateEnrollToken()
		addLog("SECURITY: Regenerated Reverse Enrollment Token.")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"enroll_token": newTok})
	})

	http.HandleFunc("/sentinel-info", func(w http.ResponseWriter, r *http.Request) {
		ramMB, _ := getMemoryStats()
		cc, qd := checkBBRStatus()
		l4, dir, wss, grpcP := getControlPorts()
		mu.Lock()
		tuns := append([]Tunnel{}, db.Tunnels...)
		recentLogs := append([]string{}, logs...)
		mu.Unlock()
		if len(recentLogs) > 25 {
			recentLogs = recentLogs[len(recentLogs)-25:]
		}
		rows := ""
		for _, t := range tuns {
			portBadges := ""
			for _, p := range parsePortList(t.InboundPorts) {
				if c, err := net.DialTimeout("tcp", "127.0.0.1:"+p, 500*time.Millisecond); err == nil {
					c.Close()
					portBadges += fmt.Sprintf(`<span style="color:#10b981;margin-right:8px;">● Forwarding :%s (BOUND)</span>`, p)
				} else {
					portBadges += fmt.Sprintf(`<span style="color:#f43f5e;margin-right:8px;">● Forwarding :%s (IDLE)</span>`, p)
				}
			}
			rows += fmt.Sprintf(`<div style="background:#080d14;padding:12px;border-radius:10px;border:1px solid #1e293b;margin-bottom:8px;"><b>[%s] %s</b> (%s) | Tunnel Port: <code>:%s</code><br><div style="margin-top:6px;font-size:12px;">Local Inbound Check: %s</div></div>`, strings.ToUpper(t.Method), t.Name, t.Role, t.TunnelPort, portBadges)
		}
		if rows == "" {
			rows = `<div style="color:#64748b;">No active tunnels configured on this node yet.</div>`
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>Sentinel Node Info</title><style>body{background:#0b111a;color:#e2e8f0;font-family:monospace;padding:20px;max-width:900px;margin:auto;}.box{background:#111a27;border:1px solid #1e293b;border-radius:16px;padding:18px;margin-bottom:16px;}h2{color:#38bdf8;margin-top:0;}</style></head><body><div class="box"><h2>🦅 Ghoghnoos Tunnel %s — Sentinel Node Status</h2><p><b>Server IP:</b> %s | <b>Panel:</b> :%s | <b>Control Ports:</b> :%s / :%s / :%s / :%s | <b>RAM:</b> %d MB | <b>BBR:</b> %s (%s)</p><p style="word-break:break-all;"><b>SHA-256 Pin:</b> <code style="color:#10b981;">%s</code></p></div><div class="box"><h3>Active Tunnel Listeners</h3>%s</div><div class="box"><h3>Recent Node Logs</h3><pre style="background:#05080d;padding:14px;border-radius:10px;color:#10b981;overflow-x:auto;">%s</pre></div></body></html>`, appVersion, localIP, port, l4, dir, wss, grpcP, ramMB, cc, qd, localPinHex, rows, strings.Join(recentLogs, "\n"))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.Trim(r.URL.Path, "/")
		if strings.HasSuffix(reqPath, "login") {
			handleLoginRoute(w, r, panelPath)
			return
		}
		if strings.HasSuffix(reqPath, "logout") {
			handleLogoutRoute(w, r, panelPath)
			return
		}
		if panelPath != "" && reqPath != "" && reqPath != panelPath && !strings.HasSuffix(reqPath, ".js") {
			http.NotFound(w, r)
			return
		}
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		if strings.HasSuffix(reqPath, "i18n.js") {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(i18nJS)
			return
		}
		if strings.HasSuffix(reqPath, "app.js") {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(appJS)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		activeTab := r.URL.Query().Get("tab")
		rangeStr := r.URL.Query().Get("range")
		ramMB, goroutines := getMemoryStats()
		l4Port, dirPort, wssPort, grpcPort := getControlPorts()

		mu.Lock()
		serversCopy := make([]ServerNode, len(db.Servers))
		for i, s := range db.Servers {
			serversCopy[i] = s
		}
		tunnelsCopy := append([]Tunnel{}, db.Tunnels...)
		prefsCopy := db.Prefs
		enrollTok := db.EnrollToken
		updAvail := updateAvailable
		var recentLocalLogs []string
		if len(logs) > 35 {
			recentLocalLogs = append([]string{}, logs[len(logs)-35:]...)
		} else {
			recentLocalLogs = append([]string{}, logs...)
		}
		metricsCopy := sysMetrics
		if activeTab == "logs" || activeTab == "" {
			metricsCopy.DailyUsage = append([]DailyBW{}, db.DailyUsage...)
		}
		var pingSummary map[string]interface{}
		if activeTab == "logs" {
			pingSummary = buildPingSummary(db.PingHistory, targetSrvID, rangeStr)
		}
		mu.Unlock()

		activeLogs, activePort, activePath, activePin := recentLocalLogs, port, panelPath, localPinHex
		srvTime := time.Now().UTC().Format("15:04:05 UTC")

		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			tok := getServerAuthToken(srv)
			cachedHB, hasCached := remoteStateCache[targetSrvID]
			mu.Unlock()
			if srv != nil && srv.IP != "" {
				client := &http.Client{Timeout: 1800 * time.Millisecond}
				req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%s/api/state?tab=logs", srv.IP, srv.PanelPort), nil)
				req.Header.Set("X-Ghq-Token", tok)
				if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
					defer resp.Body.Close()
					var rem map[string]interface{}
					if json.NewDecoder(resp.Body).Decode(&rem) == nil {
						if rl, ok := rem["logs"].([]interface{}); ok {
							activeLogs = []string{}
							for _, item := range rl {
								activeLogs = append(activeLogs, fmt.Sprintf("%v", item))
							}
							if len(activeLogs) > 35 {
								activeLogs = activeLogs[len(activeLogs)-35:]
							}
						}
						if rm, ok := rem["metrics"].(map[string]interface{}); ok {
							b, _ := json.Marshal(rm)
							_ = json.Unmarshal(b, &metricsCopy)
						}
						if rp, ok := rem["panel_port"].(string); ok {
							activePort = rp
						}
						if rpath, ok := rem["panel_path"].(string); ok {
							activePath = rpath
						}
						if rpin, ok := rem["cert_pin"].(string); ok && rpin != "" {
							activePin = rpin
							mu.Lock()
							if sPtr := findServerByID(targetSrvID); sPtr != nil && sPtr.CertPin != rpin {
								sPtr.CertPin = rpin
								syncServerTunnelsPinLocked(targetSrvID, rpin)
								saveDBLocked()
							}
							mu.Unlock()
						}
						if rtime, ok := rem["server_time"].(string); ok && rtime != "" {
							srvTime = rtime
						}
					}
				} else {
					if resp != nil {
						resp.Body.Close()
					}
					if hasCached {
						if len(cachedHB.Logs) > 0 {
							activeLogs = cachedHB.Logs
						}
						metricsCopy = cachedHB.Metrics
						if cachedHB.PanelPort != "" {
							activePort = cachedHB.PanelPort
						}
						if cachedHB.CertPin != "" {
							activePin = cachedHB.CertPin
						}
						if cachedHB.ServerTime != "" {
							srvTime = cachedHB.ServerTime
						}
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": appVersion, "update_available": updAvail, "ram_mb": ramMB, "goroutines": goroutines,
			"server_time": srvTime, "metrics": metricsCopy, "servers": serversCopy, "tunnels": tunnelsCopy,
			"ping_summary": pingSummary, "prefs": prefsCopy, "logs": activeLogs, "panel_port": activePort,
			"panel_path": activePath, "cert_pin": activePin, "enroll_token": enrollTok,
			"master_ip": localIP, "dial_port": port, "ctrl_l4_port": l4Port, "ctrl_direct_port": dirPort,
			"ctrl_wss_port": wssPort, "ctrl_grpc_port": grpcPort, "ctrl_methods": strings.Join(getAllowedControlMethods(), ","),
		})
	})

	http.HandleFunc("/api/bbr", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			tok := getServerAuthToken(srv)
			mu.Unlock()
			if srv != nil {
				client := &http.Client{Timeout: 3 * time.Second}
				req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/api/bbr", srv.IP, srv.PanelPort), nil)
				req.Header.Set("X-Ghq-Token", tok)
				if resp, err := client.Do(req); err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						addLog(fmt.Sprintf("BBR-OK [%s]: Enabled Google BBR + FQ on %s!", srv.Name, srv.IP))
						w.WriteHeader(http.StatusOK)
						return
					}
				}
			}
		}
		if err := enableLocalBBR(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		cc, qd := checkBBRStatus()
		mu.Lock()
		sysMetrics.BBRStatus, sysMetrics.Qdisc = cc, qd
		mu.Unlock()
		addLog(fmt.Sprintf("BBR-OK: Kernel TCP Congestion Control set to %s (qdisc: %s)", cc, qd))
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/uninstall", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			go func() {
				time.Sleep(400 * time.Millisecond)
				_ = exec.Command("bash", "-c", "systemctl disable ghoghnoos 2>/dev/null; rm -f /etc/systemd/system/ghoghnoos.service; systemctl daemon-reload; rm -rf /opt/ghoghnoos /usr/local/bin/ghoghnoos; systemctl stop ghoghnoos").Run()
				os.Exit(0)
			}()
		}
	})

	http.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		if r.Method == http.MethodDelete {
			id := r.URL.Query().Get("id")
			if id == "local" {
				return
			}
			mu.Lock()
			var removedSrv *ServerNode
			var updated []ServerNode
			for _, s := range db.Servers {
				if s.ID == id {
					sCopy := s
					removedSrv = &sCopy
				} else {
					updated = append(updated, s)
				}
			}
			db.Servers = updated
			if db.Prefs.SelectedServer == id {
				db.Prefs.SelectedServer = "local"
			}
			saveDBLocked()
			mu.Unlock()
			if removedSrv != nil {
				go uninstallSentinelRemote(*removedSrv)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	})

	http.HandleFunc("/api/servers/probe", func(w http.ResponseWriter, r *http.Request) {
		handleServerProbeRoute(w, r, panelPath)
	})

	http.HandleFunc("/api/prefs", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		var p UIPrefs
		if json.NewDecoder(r.Body).Decode(&p) == nil {
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
		if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
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
			tok := getServerAuthToken(srv)
			mu.Unlock()
			if srv != nil {
				client := &http.Client{Timeout: 2500 * time.Millisecond}
				body, _ := json.Marshal(map[string]string{"cmd": req.Cmd})
				reqMsg, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/api/cmd", srv.IP, srv.PanelPort), bytes.NewReader(body))
				reqMsg.Header.Set("Content-Type", "application/json")
				reqMsg.Header.Set("X-Ghq-Token", tok)
				if resp, err := client.Do(reqMsg); err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						w.WriteHeader(http.StatusOK)
						return
					}
				}
				mu.Lock()
				pendingRemoteCmds[srv.ID] = append(pendingRemoteCmds[srv.ID], req.Cmd)
				mu.Unlock()
				addLog(fmt.Sprintf("CMD-QUEUED [%s]: '%s' queued for Control Link", srv.Name, req.Cmd))
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		executeLocalConsoleCmd(req.Cmd, port)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		if r.Method == http.MethodDelete {
			id := r.URL.Query().Get("id")
			stopTunnelIfRunning(id)
			updateTunnelLimiter(id, "0")
			mu.Lock()
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
		if t.Method == "" || t.Method == "spoof" || t.Method == "tls" {
			if t.Method == "spoof" || t.Method == "tls" {
				t.Method = "direct"
			} else {
				t.Method = "wss"
			}
		}
		if !strings.HasPrefix(t.WSPath, "/") {
			t.WSPath = "/" + strings.Trim(t.WSPath, "/")
		}
		if t.TunnelPort == "" {
			t.TunnelPort = "10201"
		}
		t.InboundPorts = strings.Join(parsePortList(t.InboundPorts), ", ")
		if t.ID == "" {
			t.ID = fmt.Sprintf("%s-%d", t.Method, time.Now().Unix()%10000)
		}

		mu.Lock()
		var srcCopy, dstCopy *ServerNode
		if s := findServerByID(t.SourceServer); s != nil {
			c := *s
			srcCopy = &c
		}
		if d := findServerByID(t.TargetServer); d != nil {
			c := *d
			dstCopy = &c
		}
		if srcCopy != nil && dstCopy != nil && t.Role != "server" {
			t.Role = "client"
			t.RemoteAddr = fmt.Sprintf("%s:%s", dstCopy.IP, t.TunnelPort)
			if t.CertPin == "" && dstCopy.CertPin != "" {
				t.CertPin = dstCopy.CertPin
			}
		}
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
		srcTok := getServerAuthToken(srcCopy)
		saveDBLocked()
		mu.Unlock()

		if srcCopy != nil && dstCopy != nil && t.Role != "server" {
			exitTunnel := Tunnel{
				ID: t.ID + "-exit", Name: t.Name + " (Exit)", Role: "server", Method: t.Method,
				TunnelPort: t.TunnelPort, InboundPorts: t.InboundPorts, SNI: t.SNI,
				Fingerprint: t.Fingerprint, FragSize: t.FragSize, SpeedLimit: t.SpeedLimit, WSPath: t.WSPath, Status: "Online",
			}
			if dstCopy.IsLocal {
				dispatchTunnelEngine(exitTunnel)
			} else {
				if syncedPin := pushExitTunnelToRemote(*dstCopy, exitTunnel, t.Name); syncedPin != "" && t.CertPin == "" {
					t.CertPin = syncedPin
					mu.Lock()
					for i := range db.Tunnels {
						if db.Tunnels[i].ID == t.ID {
							db.Tunnels[i].CertPin = syncedPin
							break
						}
					}
					saveDBLocked()
					mu.Unlock()
				}
			}
		}

		if srcCopy == nil || srcCopy.IsLocal {
			dispatchTunnelEngine(t)
			go func(tun Tunnel) {
				time.Sleep(350 * time.Millisecond)
				probeTunnelOnce(tun)
			}(t)
		} else {
			body, _ := json.Marshal(t)
			reqMsg, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/api/tunnel", srcCopy.IP, srcCopy.PanelPort), bytes.NewReader(body))
			reqMsg.Header.Set("Content-Type", "application/json")
			reqMsg.Header.Set("X-Ghq-Token", srcTok)
			_, _ = (&http.Client{Timeout: 3 * time.Second}).Do(reqMsg)
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) {
			return
		}
		id := r.URL.Query().Get("id")
		mu.Lock()
		var tun *Tunnel
		for i := range db.Tunnels {
			if db.Tunnels[i].ID == id {
				tCopy := db.Tunnels[i]
				tun = &tCopy
				break
			}
		}
		mu.Unlock()
		if tun == nil {
			http.Error(w, "Not found", 404)
			return
		}
		pt := probeTunnelOnce(*tun)
		if !pt.OK {
			addLog(fmt.Sprintf("PING-WARN [%s]: %s (%d ms)", tun.Name, pt.Error, pt.LatencyMS))
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "ms": pt.LatencyMS, "error": pt.Error})
			return
		}
		addLog(fmt.Sprintf("PING-OK [%s]: Verified in %d ms!", tun.Name, pt.LatencyMS))
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "ms": pt.LatencyMS})
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		if req.CtrlL4 != "" {
			db.CtrlL4Port = req.CtrlL4
		}
		if req.CtrlDirect != "" {
			db.CtrlDirectPort = req.CtrlDirect
		}
		if req.CtrlWSS != "" {
			db.CtrlWSSPort = req.CtrlWSS
		}
		if req.CtrlGRPC != "" {
			db.CtrlGRPCPort = req.CtrlGRPC
		}
		if req.CtrlMethods != "" {
			db.CtrlMethods = req.CtrlMethods
		}
		saveDBLocked()
		mu.Unlock()
		_ = updateConfigFile(req)
		w.WriteHeader(http.StatusOK)
		go func() { time.Sleep(1 * time.Second); os.Exit(0) }()
	})
}
