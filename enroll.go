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
	"strconv"
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

// Starts dedicated Master Control Listeners on BasePort+1 (:6002 Direct-TLS) and BasePort+2 (:6003 WSS)
func startMasterControlListeners(basePortStr string) {
	if strings.Trim(os.Getenv("PANEL_PATH"), "/") == "sentinel" || os.Getenv("MASTER_ADDR") != "" {
		return
	}

	basePort, err := strconv.Atoi(strings.TrimSpace(basePortStr))
	if err != nil || basePort <= 0 {
		basePort = 6001
	}
	directPort := strconv.Itoa(basePort + 1) // 6002
	wssPort := strconv.Itoa(basePort + 2)    // 6003

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{localCert},
		MinVersion:   tls.VersionTLS12,
	}

	// 1. Dedicated Direct-TLS 1.3 Control Listener (:6002)
	go func() {
		ln, err := tls.Listen("tcp", ":"+directPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("WARN [Control-DirectTLS]: Could not bind :%s -> %v", directPort, err))
			return
		}
		addLog(fmt.Sprintf("CONTROL-READY: Direct-TLS 1.3 Ingress listening on :%s", directPort))
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleMasterControlConn(conn, "direct")
		}
	}()

	// 2. Dedicated WSS WebSocket Control Listener (:6003)
	go func() {
		ln, err := tls.Listen("tcp", ":"+wssPort, tlsCfg)
		if err != nil {
			addLog(fmt.Sprintf("WARN [Control-WSS]: Could not bind :%s -> %v", wssPort, err))
			return
		}
		addLog(fmt.Sprintf("CONTROL-READY: WSS WebSocket Ingress listening on :%s", wssPort))
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleMasterControlConn(conn, "wss")
		}
	}()
}

func handleMasterControlConn(conn net.Conn, mode string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	br := bufio.NewReader(conn)

	if mode == "direct" {
		line, err := br.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "ENROLL|") {
			return
		}
		rawB64 := strings.TrimSpace(strings.TrimPrefix(line, "ENROLL|"))
		if decoded, err := base64.StdEncoding.DecodeString(rawB64); err == nil {
			var hb EnrollHeartbeat
			if json.Unmarshal(decoded, &hb) == nil {
				hb.ViaMethod = "DIRECT"
				rep := processIncomingEnroll(hb)
				b, _ := json.Marshal(rep)
				conn.Write(append(b, '\n'))
			}
		}
		return
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "x-enroll-payload:") {
			rawB64 := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if decoded, err := base64.StdEncoding.DecodeString(rawB64); err == nil {
				var hb EnrollHeartbeat
				if json.Unmarshal(decoded, &hb) == nil {
					hb.ViaMethod = "WSS"
					rep := processIncomingEnroll(hb)
					b, _ := json.Marshal(rep)
					conn.Write(append(b, '\n'))
					return
				}
			}
		}
	}
}

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
		name := strings.TrimSpace(hb.NodeName)
		if name == "" || name == "Sentinel-Node" || strings.HasPrefix(name, "Sentinel-") {
			name = fmt.Sprintf("Server-%d", len(db.Servers)+1)
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

	if remoteStateCache == nil {
		remoteStateCache = make(map[string]EnrollHeartbeat)
	}
	remoteStateCache[targetSrv.ID] = hb

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

func resolveControlTarget(masterAddr, method string) string {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(masterAddr))
	if err != nil {
		host = strings.TrimSpace(masterAddr)
		portStr = "6001"
	}
	basePort, err := strconv.Atoi(portStr)
	if err != nil || basePort <= 0 {
		basePort = 6001
	}
	switch method {
	case "direct":
		return net.JoinHostPort(host, strconv.Itoa(basePort+1))
	case "wss":
		return net.JoinHostPort(host, strconv.Itoa(basePort+2))
	default:
		return net.JoinHostPort(host, strconv.Itoa(basePort))
	}
}

func tryDialMasterControl(masterAddr, token, nodeName, method string) (EnrollReply, int64, error) {
	var empty EnrollReply
	start := time.Now()
	targetAddr := resolveControlTarget(masterAddr, method)

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

	if method == "direct" || method == "wss" {
		dialer := &net.Dialer{Timeout: 6 * time.Second}
		rawConn, err := dialer.Dial("tcp", targetAddr)
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
			return empty, 0, fmt.Errorf("handshake rejected")
		}
		return rep, time.Since(start).Milliseconds(), nil
	}

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://%s/api/enroll", targetAddr), "application/json", strings.NewReader(string(payloadBytes)))
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

// Interactive ANSI CLI Matrix ordered strictly by Stealth Score: WSS (85%) -> Direct-TLS (75%) -> L4 API (20%)
func runCLIEnrollmentMatrix(masterAddr, token, nodeName string) {
	initSelfSignedCert()
	localIP = detectLocalIP()
	if nodeName == "" {
		nodeName = "Server-2"
	}

	wTarget := resolveControlTarget(masterAddr, "wss")
	dTarget := resolveControlTarget(masterAddr, "direct")
	fTarget := resolveControlTarget(masterAddr, "forward")

	fmt.Println("\033[1;36m┌── Ghoghnoos Sentinel Dial-In Matrix (Stealth Priority) ─────────┐\033[0m")
	fmt.Printf("\033[1;36m│\033[0m Master Base   : %-47s \033[1;36m│\033[0m\n", masterAddr)
	fmt.Printf("\033[1;36m│\033[0m Sentinel Node : %-47s \033[1;36m│\033[0m\n", nodeName+" ("+localIP+")")
	fmt.Println("\033[1;36m│                                                                 │\033[0m")

	methods := []struct {
		id     string
		label  string
		target string
	}{
		{"wss", "WSS (85% Stealth)", wTarget},
		{"direct", "Direct-TLS (75%) ", dTarget},
		{"forward", "L4 API (20% Raw) ", fTarget},
	}

	var winningMethod string
	var winningReply EnrollReply
	maxAttempts := 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			fmt.Printf("\033[1;36m│\033[0m \033[1;33m» Retry Attempt %d of %d: Testing transports again...\033[0m            \033[1;36m│\033[0m\n", attempt, maxAttempts)
		}

		for idx, m := range methods {
			_, portOnly, _ := net.SplitHostPort(m.target)
			fmt.Printf("\033[1;36m│\033[0m [%d/3] %-17s (:%-5s) ... ", idx+1, m.label, portOnly)
			rep, ms, err := tryDialMasterControl(masterAddr, token, nodeName, m.id)
			if err == nil && rep.OK {
				fmt.Printf("\033[1;32m[✔ Handshake OK | %d ms]\033[0m \033[1;36m│\033[0m\n", ms)
				winningMethod = m.id
				winningReply = rep
				break
			} else {
				fmt.Printf("\033[1;31m[✘ Filtered / Timeout]\033[0m   \033[1;36m│\033[0m\n")
			}
		}

		if winningMethod != "" {
			break
		}

		if attempt < maxAttempts {
			fmt.Println("\033[1;36m│\033[0m \033[1;33m! All 3 transports failed. Waiting 20s before retry...\033[0m          \033[1;36m│\033[0m")
			time.Sleep(20 * time.Second)
		}
	}

	fmt.Println("\033[1;36m│                                                                 │\033[0m")
	if winningMethod == "" {
		fmt.Println("\033[1;31m│ ✘ All 3 attempts failed. Check Master firewall (ports 6001-6003)│\033[0m")
		fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────────┘\033[0m")
		os.Exit(1)
	}

	fmt.Printf("\033[1;36m│\033[0m \033[1;32m» Token Verified! Master SHA-256 Pin: %s...\033[0m \033[1;36m│\033[0m\n", winningReply.MasterPin[:18])
	fmt.Printf("\033[1;36m│\033[0m \033[1;32m» Active Control Highway: %-37s\033[0m \033[1;36m│\033[0m\n", strings.ToUpper(winningMethod))
	fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────────┘\033[0m")

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

// Background Survival Loop with Stealth Hierarchy (WSS -> Direct -> Forward) & 12-Hour Opportunistic Upgrade
func startSentinelSurvivalLoop() {
	masterAddr := strings.TrimSpace(os.Getenv("MASTER_ADDR"))
	token := strings.TrimSpace(os.Getenv("ENROLL_TOKEN"))
	nodeName := strings.TrimSpace(os.Getenv("NODE_NAME"))
	activeMethod := strings.TrimSpace(os.Getenv("ACTIVE_METHOD"))
	if masterAddr == "" || token == "" {
		return
	}
	if activeMethod == "" {
		activeMethod = "wss"
	}
	addLog(fmt.Sprintf("SURVIVAL: Reverse Control Link active -> Master %s (via %s)", masterAddr, strings.ToUpper(activeMethod)))

	// Ordered from highest stealth (85%) to lowest stealth (20%)
	order := []string{"wss", "direct", "forward"}
	appliedExits := make(map[string]string)
	lastUpgradeCheck := time.Now()

	for {
		// Every 12 hours, if running on a lower-stealth transport, opportunistically test higher-stealth methods
		if activeMethod != "wss" && time.Since(lastUpgradeCheck) >= 12*time.Hour {
			lastUpgradeCheck = time.Now()
			for _, candidate := range order {
				if candidate == activeMethod {
					break // Only test methods more stealthy than the current active method
				}
				if rUp, _, errUp := tryDialMasterControl(masterAddr, token, nodeName, candidate); errUp == nil && rUp.OK {
					prev := activeMethod
					activeMethod = candidate
					saveEnrollmentConfig(masterAddr, token, nodeName, activeMethod)
					addLog(fmt.Sprintf("SURVIVAL-UPGRADE: Opportunistic 12h check upgraded Control Highway %s ➔ %s!", strings.ToUpper(prev), strings.ToUpper(activeMethod)))
					break
				}
			}
		}

		rep, _, err := tryDialMasterControl(masterAddr, token, nodeName, activeMethod)
		if err != nil || !rep.OK {
			addLog(fmt.Sprintf("SURVIVAL-WARN: Transport %s unreachable, hunting fallback by stealth priority...", strings.ToUpper(activeMethod)))
			for _, candidate := range order {
				if r2, _, err2 := tryDialMasterControl(masterAddr, token, nodeName, candidate); err2 == nil && r2.OK {
					activeMethod = candidate
					rep = r2
					err = nil
					saveEnrollmentConfig(masterAddr, token, nodeName, activeMethod)
					addLog(fmt.Sprintf("SURVIVAL-OK: Switched Control Highway to %s!", strings.ToUpper(activeMethod)))
					break
				}
			}
		}

		if err == nil && rep.OK {
			for _, cmd := range rep.PendingCmds {
				executeLocalConsoleCmd(cmd, os.Getenv("PANEL_PORT"))
			}
			for _, et := range rep.ExitTunnels {
				sig := fmt.Sprintf("%s:%s:%s:%s", et.Method, et.TunnelPort, et.InboundPorts, et.SpeedLimit)
				if appliedExits[et.ID] != sig {
					appliedExits[et.ID] = sig
					dispatchTunnelEngine(et)
				}
			}
		}

		jitterBig, _ := rand.Int(rand.Reader, big.NewInt(25))
		sleepSec := 60 + int(jitterBig.Int64())
		time.Sleep(time.Duration(sleepSec) * time.Second)
	}
}
