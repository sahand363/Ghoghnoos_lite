package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

func registerRoutes(port, panelPath string) {
	http.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(appJS)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.Trim(r.URL.Path, "/")
		if strings.HasSuffix(reqPath, "app.js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(appJS)
			return
		}
		if panelPath != "" && reqPath != "" && reqPath != panelPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(indexHTML)
	})

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
			addLog(fmt.Sprintf("SERVERS: %d server(s) linked.", len(db.Servers)))
			mu.Unlock()
		case "tunnels":
			mu.Lock()
			addLog(fmt.Sprintf("TUNNELS: %d active tunnel(s).", len(db.Tunnels)))
			mu.Unlock()
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
		w.WriteHeader(http.StatusOK)
	})

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
		t.XUIPort = strings.Join(parsePortList(t.XUIPort), ", ")
		t.ListenPort = strings.Join(parsePortList(t.ListenPort), ", ")
		if t.ID == "" {
			t.ID = fmt.Sprintf("wss-%d", time.Now().Unix()%10000)
		}

		mu.Lock()
		srcSrv := findServerByID(t.SourceServer)
		dstSrv := findServerByID(t.TargetServer)
		mu.Unlock()

		// Configure Exit Server & Entry Server cleanly without overwriting XUIPort!
		if srcSrv != nil && dstSrv != nil && t.Role != "server" {
			t.Role = "client"
			t.RemoteAddr = fmt.Sprintf("%s:%s", dstSrv.IP, t.WSSPort)
			if t.CertPin == "" && dstSrv.CertPin != "" {
				t.CertPin = dstSrv.CertPin
			}

			exitTunnel := Tunnel{
				ID:         t.ID + "-exit",
				Name:       t.Name + " (Exit)",
				Role:       "server",
				Method:     "wss",
				WSSPort:    t.WSSPort,
				ListenPort: t.WSSPort,
				XUIPort:    t.XUIPort,
				SNI:        t.SNI,
				WSPath:     t.WSPath,
				Status:     "Online",
			}

			if dstSrv.IsLocal {
				go startTunnelEngine(exitTunnel)
			} else {
				body, _ := json.Marshal(exitTunnel)
				resp, err := http.Post(fmt.Sprintf("http://%s:%s/api/tunnel", dstSrv.IP, dstSrv.PanelPort), "application/json", bytes.NewReader(body))
				if err == nil {
					resp.Body.Close()
					addLog(fmt.Sprintf("EXIT-OK [%s]: WSS Bridge :%s -> 127.0.0.1:[%s] on %s", t.Name, t.WSSPort, t.XUIPort, dstSrv.Name))
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
			go startTunnelEngine(t)
		} else {
			body, _ := json.Marshal(t)
			http.Post(fmt.Sprintf("http://%s:%s/api/tunnel", srcSrv.IP, srcSrv.PanelPort), "application/json", bytes.NewReader(body))
		}
		w.WriteHeader(http.StatusOK)
	})

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
			http.Error(w, "Not found", 404)
			return
		}

		start := time.Now()
		tlsCfg := &tls.Config{ServerName: tun.SNI, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		defer conn.Close()

		keyBytes := make([]byte, 16)
		rand.Read(keyBytes)
		wsKey := base64.StdEncoding.EncodeToString(keyBytes)
		req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nX-Target-Port: ping\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
			tun.WSPath, tun.SNI, wsKey)
		conn.Write([]byte(req))

		br := bufio.NewReader(conn)
		statusLine, err := br.ReadString('\n')
		ms := time.Since(start).Milliseconds()
		if err != nil || !strings.Contains(statusLine, "101") {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "ms": ms})
			return
		}
		addLog(fmt.Sprintf("PING-OK [%s]: %d ms", tun.Name, ms))
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "ms": ms})
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)
		_ = updateConfigFile(req.Port, req.Path)
		w.WriteHeader(http.StatusOK)
		go func() { time.Sleep(1 * time.Second); os.Exit(0) }()
	})
}
