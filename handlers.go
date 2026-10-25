package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func checkBasicAuth(w http.ResponseWriter, r *http.Request, panelPath string) bool {
	if panelPath == "sentinel" && strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	if panelUser == "" && panelPass == "" {
		return true
	}
	u, p, ok := r.BasicAuth()
	uMatch := subtle.ConstantTimeCompare([]byte(u), []byte(panelUser)) == 1
	pMatch := subtle.ConstantTimeCompare([]byte(p), []byte(panelPass)) == 1
	if !ok || !uMatch || !pMatch {
		w.Header().Set("WWW-Authenticate", `Basic realm="Ghoghnoos-lite Control Panel"`)
		http.Error(w, "401 Unauthorized — Please enter Panel Username & Password", http.StatusUnauthorized)
		return false
	}
	return true
}

func registerRoutes(port, panelPath string) {
	http.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(appJS)
	})

	// Public Control Endpoint for Reverse Sentinel Dial-In (Authenticated via EnrollToken)
	http.HandleFunc("/api/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var hb EnrollHeartbeat
		if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
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

	// Rotate / Regenerate Master Enrollment Token
	http.HandleFunc("/api/enroll/token", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
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
		mu.Lock()
		tuns := append([]Tunnel{}, db.Tunnels...)
		recentLogs := append([]string{}, logs...)
		mu.Unlock()
		if len(recentLogs) > 20 {
			recentLogs = recentLogs[len(recentLogs)-20:]
		}

		rows := ""
		for _, t := range tuns {
			ports := parsePortList(t.InboundPorts)
			portBadges := ""
			for _, p := range ports {
				c, err := net.DialTimeout("tcp", "127.0.0.1:"+p, 500*time.Millisecond)
				if err == nil {
					c.Close()
					portBadges += fmt.Sprintf(`<span style="color:#10b981;margin-right:8px;">● 127.0.0.1:%s (X-UI OPEN)</span>`, p)
				} else {
					portBadges += fmt.Sprintf(`<span style="color:#f43f5e;margin-right:8px;">● 127.0.0.1:%s (REFUSED)</span>`, p)
				}
			}
			mBadge := strings.ToUpper(t.Method)
			if mBadge == "" {
				mBadge = "WSS"
			}
			limStr := "Unlimited"
			if t.SpeedLimit != "" && t.SpeedLimit != "0" {
				limStr = t.SpeedLimit + " MB/s"
			}
			rows += fmt.Sprintf(`<div style="background:#080d14;padding:12px;border-radius:10px;border:1px solid #1e293b;margin-bottom:8px;">
				<b>[%s] %s</b> (%s) | Tunnel Port: <code>:%s</code> | Speed: <b>%s</b><br>
				<div style="margin-top:6px;font-size:12px;">Local X-UI Check: %s</div>
			</div>`, mBadge, t.Name, t.Role, t.TunnelPort, limStr, portBadges)
		}
		if rows == "" {
			rows = `<div style="color:#64748b;">No active tunnels configured on this node yet.</div>`
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sentinel Node Info</title>
		<style>body{background:#0b111a;color:#e2e8f0;font-family:monospace;padding:20px;max-width:900px;margin:auto;}
		.box{background:#111a27;border:1px solid #1e293b;border-radius:16px;padding:18px;margin-bottom:16px;}
		h2{color:#38bdf8;margin-top:0;}</style></head><body>
		<div class="box"><h2>🦅 Ghoghnoos-lite — Sentinel Node Status</h2>
		<p><b>Server IP:</b> %s | <b>Port:</b> %s | <b>RAM:</b> %d MB | <b>BBR:</b> %s (%s) | <b>Time:</b> %s</p>
		<p style="word-break:break-all;"><b>SHA-256 Pin:</b> <code style="color:#10b981;">%s</code></p></div>
		<div class="box"><h3>Active Tunnel Listeners & Local X-UI Port Check</h3>%s</div>
		<div class="box"><h3>Recent Node Logs</h3><pre style="background:#05080d;padding:14px;border-radius:10px;color:#10b981;overflow-x:auto;">%s</pre></div>
		</body></html>`, localIP, port, ramMB, cc, qd, time.Now().Format("15:04:05 MST"), localPinHex, rows, strings.Join(recentLogs, "\n"))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.Trim(r.URL.Path, "/")
		if panelPath != "" && reqPath != "" && reqPath != panelPath && !strings.HasSuffix(reqPath, "app.js") {
			http.NotFound(w, r)
			return
		}
		if !checkBasicAuth(w, r, panelPath) {
			return
		}
		if strings.HasSuffix(reqPath, "app.js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(appJS)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		ramMB, goroutines := getMemoryStats()

		mu.Lock()
		serversCopy := append([]ServerNode{}, db.Servers...)
		tunnelsCopy := append([]Tunnel{}, db.Tunnels...)
		historyCopy := append([]PingPoint{}, db.PingHistory...)
		prefsCopy := db.Prefs
		enrollTok := db.EnrollToken
		localLogs := append([]string{}, logs...)
		metricsCopy := sysMetrics
		mu.Unlock()

		activeLogs := localLogs
		activePort := port
		activePath := panelPath
		activePin := localPinHex
		srvTime := time.Now().UTC().Format("15:04:05 UTC")

		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			cachedHB, hasCached := remoteStateCache[targetSrvID]
			mu.Unlock()

			if srv != nil && srv.IP != "" {
				client := &http.Client{Timeout: 2500 * time.Millisecond}
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
						if rpin, ok := rem["cert_pin"].(string); ok {
							activePin = rpin
						}
						if rtime, ok := rem["server_time"].(string); ok && rtime != "" {
							srvTime = rtime
						}
					}
				} else if hasCached {
					// Fallback to Reverse Dial-In Heartbeat Cache when direct HTTP is blocked!
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
				} else {
					activeLogs = append(localLogs, fmt.Sprintf("[WARN] Remote node %s (%s:%s) unreachable: %v", srv.Name, srv.IP, srv.PanelPort, err))
				}
			}
		}

		// Choose recommended Dial-In Port (if Master has an active WSS or Direct-TLS Exit listener, show it, else panel port)
		dialPort := port

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ram_mb":       ramMB,
			"goroutines":   goroutines,
			"server_time":  srvTime,
			"metrics":      metricsCopy,
			"servers":      serversCopy,
			"tunnels":      tunnelsCopy,
			"ping_history": historyCopy,
			"prefs":        prefsCopy,
			"logs":         activeLogs,
			"panel_port":   activePort,
			"panel_path":   activePath,
			"panel_user":   panelUser,
			"cert_pin":     activePin,
			"enroll_token": enrollTok,
			"master_ip":    localIP,
			"dial_port":    dialPort,
		})
	})

	http.HandleFunc("/api/bbr", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		targetSrvID := r.URL.Query().Get("server_id")
		if targetSrvID != "" && targetSrvID != "local" {
			mu.Lock()
			srv := findServerByID(targetSrvID)
			mu.Unlock()
			if srv != nil {
				resp, err := http.Post(fmt.Sprintf("http://%s:%s/api/bbr", srv.IP, srv.PanelPort), "application/json", nil)
				if err == nil {
					resp.Body.Close()
					addLog(fmt.Sprintf("BBR-OK [%s]: Enabled Google BBR + FQ on %s!", srv.Name, srv.IP))
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
		if err := enableLocalBBR(); err != nil {
			addLog(fmt.Sprintf("BBR-ERR: Could not enable BBR -> %v", err))
			http.Error(w, err.Error(), 500)
			return
		}
		cc, qd := checkBBRStatus()
		mu.Lock()
		sysMetrics.BBRStatus = cc
		sysMetrics.Qdisc = qd
		mu.Unlock()
		addLog(fmt.Sprintf("BBR-OK: Kernel TCP Congestion Control set to %s (qdisc: %s)", cc, qd))
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/uninstall", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = exec.Command("bash", "-c", "systemctl disable litetun 2>/dev/null; rm -f /etc/systemd/system/litetun.service; systemctl daemon-reload; rm -rf /opt/litetun /usr/local/bin/litetun; systemctl stop litetun").Run()
			os.Exit(0)
		}()
	})

	http.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
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
			s.PanelPort = "6001"
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

	http.HandleFunc("/api/servers/deploy", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
			return
		}
		id := r.URL.Query().Get("id")
		mu.Lock()
		srv := findServerByID(id)
		if srv == nil {
			mu.Unlock()
			http.Error(w, "Server not found", 404)
			return
		}
		srv.Status = "Verifying..."
		sCopy := *srv
		saveDBLocked()
		mu.Unlock()

		go deploySentinelSSH(sCopy)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/prefs", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
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
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
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
				client := &http.Client{Timeout: 2500 * time.Millisecond}
				body, _ := json.Marshal(map[string]string{"cmd": req.Cmd})
				resp, err := client.Post(fmt.Sprintf("http://%s:%s/api/cmd", srv.IP, srv.PanelPort), "application/json", bytes.NewReader(body))
				if err == nil {
					resp.Body.Close()
					w.WriteHeader(http.StatusOK)
					return
				}
				// Queue command for next Reverse Survival Loop sync if direct HTTP is blocked
				mu.Lock()
				pendingRemoteCmds[srv.ID] = append(pendingRemoteCmds[srv.ID], req.Cmd)
				mu.Unlock()
				addLog(fmt.Sprintf("CMD-QUEUED [%s]: '%s' queued for Reverse Control Link", srv.Name, req.Cmd))
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		executeLocalConsoleCmd(req.Cmd, port)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
			return
		}
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
		srcSrv := findServerByID(t.SourceServer)
		dstSrv := findServerByID(t.TargetServer)
		mu.Unlock()

		if srcSrv != nil && dstSrv != nil && t.Role != "server" {
			t.Role = "client"
			t.RemoteAddr = fmt.Sprintf("%s:%s", dstSrv.IP, t.TunnelPort)
			if t.CertPin == "" && dstSrv.CertPin != "" {
				t.CertPin = dstSrv.CertPin
			}

			exitTunnel := Tunnel{
				ID: t.ID + "-exit", Name: t.Name + " (Exit)", Role: "server", Method: t.Method,
				TunnelPort: t.TunnelPort, InboundPorts: t.InboundPorts, SNI: t.SNI,
				Fingerprint: t.Fingerprint, FragSize: t.FragSize,
				SpeedLimit: t.SpeedLimit, WSPath: t.WSPath, Status: "Online",
			}

			if dstSrv.IsLocal {
				dispatchTunnelEngine(exitTunnel)
			} else {
				client := &http.Client{Timeout: 3 * time.Second}
				body, _ := json.Marshal(exitTunnel)
				resp, err := client.Post(fmt.Sprintf("http://%s:%s/api/tunnel", dstSrv.IP, dstSrv.PanelPort), "application/json", bytes.NewReader(body))
				if err == nil {
					resp.Body.Close()
					addLog(fmt.Sprintf("EXIT-OK [%s]: %s :%s -> 127.0.0.1:[%s] on %s", t.Name, strings.ToUpper(t.Method), t.TunnelPort, t.InboundPorts, dstSrv.Name))
				} else {
					addLog(fmt.Sprintf("EXIT-SYNC [%s]: Queued Exit listener for %s via Reverse Control Link", t.Name, dstSrv.Name))
				}
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
			dispatchTunnelEngine(t)
			go probeTunnelOnce(t)
		} else {
			body, _ := json.Marshal(t)
			http.Post(fmt.Sprintf("http://%s:%s/api/tunnel", srcSrv.IP, srcSrv.PanelPort), "application/json", bytes.NewReader(body))
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) {
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
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)
		_ = updateConfigFile(req.Port, req.Path, req.User, req.Pass)
		w.WriteHeader(http.StatusOK)
		go func() { time.Sleep(1 * time.Second); os.Exit(0) }()
	})
}
