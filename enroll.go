package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type EnrollHeartbeat struct {
	Token      string        `json:"token"`
	NodeID     string        `json:"node_id"`
	NodeName   string        `json:"node_name"`
	IP         string        `json:"ip"`
	PanelPort  string        `json:"panel_port"`
	CertPin    string        `json:"cert_pin"`
	ViaMethod  string        `json:"via_method"`
	ServerTime string        `json:"server_time"`
	Metrics    SystemMetrics `json:"metrics"`
	Logs       []string      `json:"logs"`
}

type EnrollReply struct {
	OK          bool     `json:"ok"`
	MasterPin   string   `json:"master_pin"`
	NodeID      string   `json:"node_id"`
	PendingCmds []string `json:"pending_cmds,omitempty"`
	ExitTunnels []Tunnel `json:"exit_tunnels,omitempty"`
}

func ensureEnrollToken() string {
	mu.Lock()
	defer mu.Unlock()
	if db.EnrollToken == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		db.EnrollToken = "ghq_" + hex.EncodeToString(b)
		saveDBLocked()
	}
	return db.EnrollToken
}

func regenerateEnrollToken() string {
	mu.Lock()
	defer mu.Unlock()
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	db.EnrollToken = "ghq_" + hex.EncodeToString(b)
	saveDBLocked()
	return db.EnrollToken
}

// Handles an incoming reverse enrollment / heartbeat frame on Master
func processIncomingEnroll(hb EnrollHeartbeat) EnrollReply {
	mu.Lock()
	defer mu.Unlock()

	if db.EnrollToken == "" || hb.Token != db.EnrollToken {
		return EnrollReply{OK: false}
	}

	var targetSrv *ServerNode
	for i := range db.Servers {
		if db.Servers[i].IP == hb.IP || (hb.NodeID != "" && db.Servers[i].ID == hb.NodeID) {
			targetSrv = &db.Servers[i]
			break
		}
	}

	if targetSrv == nil {
		newID := fmt.Sprintf("srv-%d", time.Now().Unix()%10000)
		name := hb.NodeName
		if name == "" {
			name = "Sentinel-" + hb.IP
		}
		newNode := ServerNode{
			ID:        newID,
			Name:      name,
			IP:        hb.IP,
			SSHPort:   "22",
			SSHUser:   "root",
			PanelPort: hb.PanelPort,
			CertPin:   hb.CertPin,
			Status:    "Online",
			ViaMethod: strings.ToUpper(hb.ViaMethod),
			LastSeen:  time.Now().Unix(),
		}
		db.Servers = append(db.Servers, newNode)
		targetSrv = &db.Servers[len(db.Servers)-1]
		go addLog(fmt.Sprintf("ENROLL-OK [%s]: Reverse Dial-In registered from %s via %s!", targetSrv.Name, hb.IP, strings.ToUpper(hb.ViaMethod)))
	} else {
		if hb.NodeName != "" && strings.HasPrefix(targetSrv.Name, "Pending") {
			targetSrv.Name = hb.NodeName
		}
		if hb.PanelPort != "" {
			targetSrv.PanelPort = hb.PanelPort
		}
		if hb.CertPin != "" {
			targetSrv.CertPin = hb.CertPin
		}
		prevMethod := targetSrv.ViaMethod
		targetSrv.ViaMethod = strings.ToUpper(hb.ViaMethod)
		targetSrv.Status = "Online"
		targetSrv.LastSeen = time.Now().Unix()
		if prevMethod != "" && prevMethod != targetSrv.ViaMethod {
			go addLog(fmt.Sprintf("SURVIVAL-SWITCH [%s]: Control link auto-migrated %s ➔ %s", targetSrv.Name, prevMethod, targetSrv.ViaMethod))
		}
	}

	// Cache latest telemetry & logs pushed by the reverse-enrolled Sentinel
	if remoteStateCache == nil {
		remoteStateCache = make(map[string]EnrollHeartbeat)
	}
	remoteStateCache[targetSrv.ID] = hb

	// Gather any Exit Tunnels assigned to this Sentinel so it stays synced even without inbound HTTP
	var assignedExitTunnels []Tunnel
	for _, t := range db.Tunnels {
		if t.TargetServer == targetSrv.ID && t.Role != "server" {
			assignedExitTunnels = append(assignedExitTunnels, Tunnel{
				ID:           t.ID + "-exit",
				Name:         t.Name + " (Exit)",
				Role:         "server",
				Method:       t.Method,
				TunnelPort:   t.TunnelPort,
				InboundPorts: t.InboundPorts,
				SNI:          t.SNI,
				Fingerprint:  t.Fingerprint,
				FragSize:     t.FragSize,
				SpeedLimit:   t.SpeedLimit,
				WSPath:       t.WSPath,
				Status:       "Online",
			})
		}
	}

	var cmds []string
	if queued, ok := pendingRemoteCmds[targetSrv.ID]; ok && len(queued) > 0 {
		cmds = append([]string{}, queued...)
		delete(pendingRemoteCmds, targetSrv.ID)
	}

	saveDBLocked()
	return EnrollReply{
		OK:          true,
		MasterPin:   localPinHex,
		NodeID:      targetSrv.ID,
		PendingCmds: cmds,
		ExitTunnels: assignedExitTunnels,
	}
}

// Try dialing Master using a specific transport method ("direct", "wss", or "forward")
func tryDialMasterControl(masterAddr, token, nodeName, method string) (EnrollReply, int64, error) {
	var empty EnrollReply
	start := time.Now()

	mu.Lock()
	logsCopy := append([]string{}, logs...)
	if len(logsCopy) > 40 {
		logsCopy = logsCopy[len(logsCopy)-40:]
	}
	metricsCopy := sysMetrics
	mu.Unlock()

	hb := EnrollHeartbeat{
		Token:      token,
		NodeName:   nodeName,
		IP:         detectLocalIP(),
		PanelPort:  os.Getenv("PANEL_PORT"),
		CertPin:    localPinHex,
		ViaMethod:  method,
		ServerTime: time.Now().UTC().Format("15:04:05 UTC"),
		Metrics:    metricsCopy,
		Logs:       logsCopy,
	}
	payloadBytes, _ := json.Marshal(hb)

	// Transport 1 & 2: Direct-TLS or WSS over TLS 1.3
	if method == "direct" || method == "wss" {
		dialer := &net.Dialer{Timeout: 6 * time.Second}
		rawConn, err := dialer.Dial("tcp", masterAddr)
		if err != nil {
			return empty, 0, err
		}
		defer rawConn.Close()

		tlsCfg := &tls.Config{
			ServerName:         "www.speedtest.net",
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}
		tlsConn := tls.Client(rawConn, tlsCfg)
		_ = tlsConn.SetDeadline(time.Now().Add(6 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			return empty, 0, err
		}

		if method == "wss" {
			keyBytes := make([]byte, 16)
			_, _ = rand.Read(keyBytes)
			wsKey := base64.StdEncoding.EncodeToString(keyBytes)
			req := fmt.Sprintf("GET /ws HTTP/1.1\r\nHost: www.speedtest.net\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nX-Enroll-Payload: %s\r\n\r\n",
				wsKey, base64.StdEncoding.EncodeToString(payloadBytes))
			if _, err := tlsConn.Write([]byte(req)); err != nil {
				return empty, 0, err
			}
		} else {
			line := fmt.Sprintf("ENROLL|%s\n", base64.StdEncoding.EncodeToString(payloadBytes))
			if _, err := tlsConn.Write([]byte(line)); err != nil {
				return empty, 0, err
			}
		}

		replyLine, err := bufio.NewReader(tlsConn).ReadString('\n')
		if err != nil {
			return empty, 0, err
		}
		var rep EnrollReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(replyLine)), &rep); err != nil || !rep.OK {
			return empty, 0, fmt.Errorf(" handshake rejected")
		}
		return rep, time.Since(start).Milliseconds(), nil
	}

	// Transport 3: HTTP / L4 Control Fallback on Master Panel Port
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://%s/api/enroll", masterAddr), "application/json", strings.NewReader(string(payloadBytes)))
	if err != nil {
		return empty, 0, err
	}
	defer resp.Body.Close()
	var rep EnrollReply
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil || !rep.OK {
		return empty, 0, fmt.Errorf("token rejected by master")
	}
	return rep, time.Since(start).Milliseconds(), nil
}

// Interactive ANSI CLI Matrix executed when user runs `--enroll <MasterIP:Port> <Token> [NodeName]`
func runCLIEnrollmentMatrix(masterAddr, token, nodeName string) {
	initSelfSignedCert()
	localIP = detectLocalIP()
	if nodeName == "" {
		nodeName = "Sentinel-" + localIP
	}

	fmt.Println("\033[1;36m┌── Ghoghnoos Sentinel Dial-In Matrix ────────────────────────────┐\033[0m")
	fmt.Printf("\033[1;36m│\033[0m Master Target : %-47s \033[1;36m│\033[0m\n", masterAddr)
	fmt.Printf("\033[1;36m│\033[0m Sentinel Node : %-47s \033[1;36m│\033[0m\n", nodeName+" ("+localIP+")")
	fmt.Println("\033[1;36m│                                                                 │\033[0m")

	methods := []struct {
		id    string
		label string
	}{
		{"direct", "Direct-TLS 1.3"},
		{"wss", "WSS WebSocket "},
		{"forward", "L4 Control API"},
	}

	var winningMethod string
	var winningReply EnrollReply

	for idx, m := range methods {
		fmt.Printf("\033[1;36m│\033[0m [%d/%d] Testing %-15s ... ", idx+1, len(methods), m.label)
		rep, ms, err := tryDialMasterControl(masterAddr, token, nodeName, m.id)
		if err == nil && rep.OK {
			fmt.Printf("\033[1;32m[✔ Handshake OK | %d ms]\033[0m      \033[1;36m│\033[0m\n", ms)
			winningMethod = m.id
			winningReply = rep
			break
		} else {
			fmt.Printf("\033[1;31m[✘ Filtered / Timeout]\033[0m        \033[1;36m│\033[0m\n")
		}
	}

	fmt.Println("\033[1;36m│                                                                 │\033[0m")
	if winningMethod == "" {
		fmt.Println("\033[1;31m│ ✘ All transport methods failed to reach Master.                 │\033[0m")
		fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────────┘\033[0m")
		os.Exit(1)
	}

	fmt.Printf("\033[1;36m│\033[0m \033[1;32m» Token Verified! Master SHA-256 Pin: %s...\033[0m \033[1;36m│\033[0m\n", winningReply.MasterPin[:18])
	fmt.Printf("\033[1;36m│\033[0m \033[1;32m» Active Control Highway: %-37s\033[0m \033[1;36m│\033[0m\n", strings.ToUpper(winningMethod))
	fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────────┘\033[0m")

	// Save enrollment state to config.env so background systemd service maintains the Survival Loop
	saveEnrollmentConfig(masterAddr, token, nodeName, winningMethod)
}

func saveEnrollmentConfig(masterAddr, token, nodeName, activeMethod string) {
	_ = os.MkdirAll("/opt/litetun", 0755)
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "6001"
	}
	content := fmt.Sprintf("PANEL_PORT=%s\nPANEL_PATH=sentinel\nPANEL_USER=admin\nPANEL_PASS=admin\nMASTER_ADDR=%s\nENROLL_TOKEN=%s\nNODE_NAME=%s\nACTIVE_METHOD=%s\n",
		port, masterAddr, token, nodeName, activeMethod)
	_ = os.WriteFile(confPath, []byte(content), 0644)
}

// Background Survival Loop on Sentinel: jittered 60-85s heartbeat + auto-transport hunting if blocked
func startSentinelSurvivalLoop() {
	masterAddr := strings.TrimSpace(os.Getenv("MASTER_ADDR"))
	token := strings.TrimSpace(os.Getenv("ENROLL_TOKEN"))
	nodeName := strings.TrimSpace(os.Getenv("NODE_NAME"))
	activeMethod := strings.TrimSpace(os.Getenv("ACTIVE_METHOD"))
	if masterAddr == "" || token == "" {
		return
	}
	if activeMethod == "" {
		activeMethod = "direct"
	}
	addLog(fmt.Sprintf("SURVIVAL: Reverse Control Link active -> Master %s (via %s)", masterAddr, strings.ToUpper(activeMethod)))

	order := []string{"direct", "wss", "forward"}
	appliedExits := make(map[string]string)

	for {
		rep, _, err := tryDialMasterControl(masterAddr, token, nodeName, activeMethod)
		if err != nil || !rep.OK {
			addLog(fmt.Sprintf("SURVIVAL-WARN: Transport %s unreachable, hunting fallback...", strings.ToUpper(activeMethod)))
			for _, candidate := range order {
				if r2, _, err2 := tryDialMasterControl(masterAddr, token, nodeName, candidate); err2 == nil && r2.OK {
					activeMethod = candidate
					rep = r2
					err = nil
					addLog(fmt.Sprintf("SURVIVAL-OK: Switched Control Highway to %s!", strings.ToUpper(activeMethod)))
					break
				}
			}
		}

		if err == nil && rep.OK {
			// Execute any console commands queued from Master's PuTTY window
			for _, cmd := range rep.PendingCmds {
				executeLocalConsoleCmd(cmd, os.Getenv("PANEL_PORT"))
			}
			// Ensure all Exit Tunnels assigned to this Sentinel are running
			for _, et := range rep.ExitTunnels {
				sig := fmt.Sprintf("%s:%s:%s:%s", et.Method, et.TunnelPort, et.InboundPorts, et.SpeedLimit)
				if appliedExits[et.ID] != sig {
					appliedExits[et.ID] = sig
					dispatchTunnelEngine(et)
				}
			}
		}

		// Jittered sleep: 60s + random(0..25s) to prevent DPI periodic-beacon detection
		jitterBig, _ := rand.Int(rand.Reader, big.NewInt(25))
		sleepSec := 60 + int(jitterBig.Int64())
		time.Sleep(time.Duration(sleepSec) * time.Second)
	}
}
