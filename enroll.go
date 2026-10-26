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
	IsMaster   bool          `json:"is_master,omitempty"`
	ServerTime string        `json:"server_time"`
	Metrics    SystemMetrics `json:"metrics"`
	Logs       []string      `json:"logs"`
}

type EnrollReply struct {
	OK          bool          `json:"ok"`
	MasterPin   string        `json:"master_pin"`
	NodeID      string        `json:"node_id"`
	NodeName    string        `json:"node_name,omitempty"`
	PanelPort   string        `json:"panel_port,omitempty"`
	ServerTime  string        `json:"server_time,omitempty"`
	Metrics     SystemMetrics `json:"metrics,omitempty"`
	Logs        []string      `json:"logs,omitempty"`
	PendingCmds []string      `json:"pending_cmds,omitempty"`
	ExitTunnels []Tunnel      `json:"exit_tunnels,omitempty"`
}

func ensureEnrollToken() string {
	mu.Lock()
	defer mu.Unlock()
	if envTok := strings.TrimSpace(os.Getenv("ENROLL_TOKEN")); envTok != "" {
		db.EnrollToken = envTok
	}
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

func parseMasterAddrSpec(spec string) (host, l4, direct, wss string) {
	parts := strings.Split(strings.TrimSpace(spec), ":")
	host = parts[0]
	defL4, defDir, defWSS := getControlPorts()
	if len(parts) >= 4 {
		return host, parts[1], parts[2], parts[3]
	}
	if len(parts) == 2 && parts[1] != "" {
		return host, parts[1], defDir, defWSS
	}
	return host, defL4, defDir, defWSS
}

// Binds all 3 dedicated Control Listeners (6005 L4, 6006 Direct-TLS, 6007 WSS) on BOTH Master and Sentinel
func startControlListeners() {
	l4Port, directPort, wssPort := getControlPorts()
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{localCert}, MinVersion: tls.VersionTLS12}

	// 1. Dedicated L4 Control Listener (:6005)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/enroll", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var hb EnrollHeartbeat
			if json.NewDecoder(r.Body).Decode(&hb) != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			hb.ViaMethod = "FORWARD"
			rep := processIncomingEnroll(hb)
			if !rep.OK {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(rep)
		})
		addLog(fmt.Sprintf("CONTROL-READY: L4 Control Ingress listening on :%s", l4Port))
		_ = http.ListenAndServe(":"+l4Port, mux)
	}()

	// 2. Dedicated Direct-TLS 1.3 Control Listener (:6006)
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
			go handleControlConn(conn, "direct")
		}
	}()

	// 3. Dedicated WSS WebSocket Control Listener (:6007)
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
			go handleControlConn(conn, "wss")
		}
	}()
}

func handleControlConn(conn net.Conn, mode string) {
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
		if strings.HasPrefix(strings.ToLower(line), "x-enroll-payload:") {
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
	expectedTok := strings.TrimSpace(os.Getenv("ENROLL_TOKEN"))
	mu.Lock()
	if expectedTok == "" {
		expectedTok = db.EnrollToken
	}
	if expectedTok != "" && hb.Token != "" && hb.Token != expectedTok {
		mu.Unlock()
		return EnrollReply{OK: false}
	}

	// If this node is a Sentinel receiving a Forward Rendezvous Probe from Master:
	if hb.IsMaster {
		logsCopy := append([]string{}, logs...)
		if len(logsCopy) > 40 {
			logsCopy = logsCopy[len(logsCopy)-40:]
		}
		mCopy := sysMetrics
		nodeName := os.Getenv("NODE_NAME")
		if nodeName == "" {
			nodeName = "Sentinel"
		}
		pPort := os.Getenv("PANEL_PORT")
		if pPort == "" {
			pPort = "6001"
		}
		mu.Unlock()
		for _, cmd := range strings.Split(hb.NodeName, "||") {
			if strings.TrimSpace(cmd) != "" {
				executeLocalConsoleCmd(cmd, pPort)
			}
		}
		return EnrollReply{
			OK: true, MasterPin: localPinHex, NodeName: nodeName,
			PanelPort: pPort, ServerTime: time.Now().UTC().Format("15:04:05 UTC"),
			Metrics: mCopy, Logs: logsCopy,
		}
	}

	var targetSrv *ServerNode
	for i := range db.Servers {
		if db.Servers[i].IP == hb.IP || (hb.NodeID != "" && db.Servers[i].ID == hb.NodeID) {
			targetSrv = &db.Servers[i]
			break
		}
	}

	if targetSrv == nil {
		name := strings.TrimSpace(hb.NodeName)
		if name == "" || strings.HasPrefix(name, "Sentinel") {
			name = fmt.Sprintf("Server-%d", len(db.Servers)+1)
		}
		newNode := ServerNode{
			ID: fmt.Sprintf("srv-%d", time.Now().Unix()%10000), Name: name, IP: hb.IP,
			SSHPort: "22", SSHUser: "root", PanelPort: hb.PanelPort, CertPin: hb.CertPin,
			Status: "Online", ViaMethod: strings.ToUpper(hb.ViaMethod), LastSeen: time.Now().Unix(),
		}
		db.Servers = append(db.Servers, newNode)
		targetSrv = &db.Servers[len(db.Servers)-1]
		go addLog(fmt.Sprintf("ENROLL-OK [%s]: Connected from %s via %s!", targetSrv.Name, hb.IP, targetSrv.ViaMethod))
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
		prev := targetSrv.ViaMethod
		targetSrv.ViaMethod = strings.ToUpper(hb.ViaMethod)
		targetSrv.Status = "Online"
		targetSrv.Probing = false
		targetSrv.LastSeen = time.Now().Unix()
		if prev != "" && prev != targetSrv.ViaMethod {
			go addLog(fmt.Sprintf("SURVIVAL-SWITCH [%s]: Control link migrated %s ➔ %s", targetSrv.Name, prev, targetSrv.ViaMethod))
		}
	}

	remoteStateCache[targetSrv.ID] = hb
	var assignedExits []Tunnel
	for _, t := range db.Tunnels {
		if t.TargetServer == targetSrv.ID && t.Role != "server" {
			assignedExits = append(assignedExits, Tunnel{
				ID: t.ID + "-exit", Name: t.Name + " (Exit)", Role: "server", Method: t.Method,
				TunnelPort: t.TunnelPort, InboundPorts: t.InboundPorts, SNI: t.SNI,
				Fingerprint: t.Fingerprint, FragSize: t.FragSize, SpeedLimit: t.SpeedLimit, WSPath: t.WSPath, Status: "Online",
			})
		}
	}
	var cmds []string
	if q, ok := pendingRemoteCmds[targetSrv.ID]; ok && len(q) > 0 {
		cmds = append([]string{}, q...)
		delete(pendingRemoteCmds, targetSrv.ID)
	}
	saveDBLocked()
	mu.Unlock()

	return EnrollReply{OK: true, MasterPin: localPinHex, NodeID: targetSrv.ID, PendingCmds: cmds, ExitTunnels: assignedExits}
}

func resolveControlTarget(masterSpec, method string) string {
	host, l4, direct, wss := parseMasterAddrSpec(masterSpec)
	switch method {
	case "direct":
		return net.JoinHostPort(host, direct)
	case "wss":
		return net.JoinHostPort(host, wss)
	default:
		return net.JoinHostPort(host, l4)
	}
}

func tryDialControl(targetSpec, token, nodeName, method string, isMaster bool) (EnrollReply, int64, error) {
	var empty EnrollReply
	start := time.Now()
	targetAddr := resolveControlTarget(targetSpec, method)

	mu.Lock()
	logsCopy := append([]string{}, logs...)
	if len(logsCopy) > 40 {
		logsCopy = logsCopy[len(logsCopy)-40:]
	}
	metricsCopy := sysMetrics
	mu.Unlock()

	hb := EnrollHeartbeat{
		Token: token, NodeName: nodeName, IP: detectLocalIP(), PanelPort: os.Getenv("PANEL_PORT"),
		CertPin: localPinHex, ViaMethod: method, IsMaster: isMaster,
		ServerTime: time.Now().UTC().Format("15:04:05 UTC"), Metrics: metricsCopy, Logs: logsCopy,
	}
	payloadBytes, _ := json.Marshal(hb)

	if method == "direct" || method == "wss" {
		rawConn, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial("tcp", targetAddr)
		if err != nil {
			return empty, 0, err
		}
		defer rawConn.Close()
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: "www.speedtest.net", InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		_ = tlsConn.SetDeadline(time.Now().Add(5 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			return empty, 0, err
		}
		if method == "wss" {
			keyBytes := make([]byte, 16)
			_, _ = rand.Read(keyBytes)
			req := fmt.Sprintf("GET /ws HTTP/1.1\r\nHost: www.speedtest.net\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nX-Enroll-Payload: %s\r\n\r\n",
				base64.StdEncoding.EncodeToString(keyBytes), base64.StdEncoding.EncodeToString(payloadBytes))
			_, _ = tlsConn.Write([]byte(req))
		} else {
			_, _ = tlsConn.Write([]byte(fmt.Sprintf("ENROLL|%s\n", base64.StdEncoding.EncodeToString(payloadBytes))))
		}
		replyLine, err := bufio.NewReader(tlsConn).ReadString('\n')
		if err != nil {
			return empty, 0, err
		}
		var rep EnrollReply
		if json.Unmarshal([]byte(strings.TrimSpace(replyLine)), &rep) != nil || !rep.OK {
			return empty, 0, fmt.Errorf("rejected")
		}
		return rep, time.Since(start).Milliseconds(), nil
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(fmt.Sprintf("http://%s/api/enroll", targetAddr), "application/json", strings.NewReader(string(payloadBytes)))
	if err != nil {
		return empty, 0, err
	}
	defer resp.Body.Close()
	var rep EnrollReply
	if json.NewDecoder(resp.Body).Decode(&rep) != nil || !rep.OK {
		return empty, 0, fmt.Errorf("rejected")
	}
	return rep, time.Since(start).Milliseconds(), nil
}

// Fast initial check (NO 20s pauses, NO os.Exit(1) on failure — always saves config so background daemon & listeners start!)
func runCLIEnrollmentMatrix(masterSpec, token, nodeName string) {
	initSelfSignedCert()
	localIP = detectLocalIP()
	if nodeName == "" {
		nodeName = "Server-2"
	}
	_, l4, dir, wss := parseMasterAddrSpec(masterSpec)
	fmt.Println("\033[1;36m┌── Ghoghnoos Sentinel Dial-In & Listener Setup ──────────────────┐\033[0m")
	fmt.Printf("\033[1;36m│\033[0m Master Target : %-47s \033[1;36m│\033[0m\n", masterSpec)
	fmt.Printf("\033[1;36m│\033[0m Control Ports : WSS :%s | Direct-TLS :%s | L4 :%-12s \033[1;36m│\033[0m\n", wss, dir, l4)

	methods := []struct{ id, label, port string }{
		{"wss", "WSS (85% Stealth)", wss},
		{"direct", "Direct-TLS (75%) ", dir},
		{"forward", "L4 API (20% Raw) ", l4},
	}
	winningMethod := "wss"
	connectedNow := false

	for idx, m := range methods {
		fmt.Printf("\033[1;36m│\033[0m [%d/3] %-17s (:%-5s) ... ", idx+1, m.label, m.port)
		if rep, ms, err := tryDialControl(masterSpec, token, nodeName, m.id, false); err == nil && rep.OK {
			fmt.Printf("\033[1;32m[✔ Connected | %d ms]\033[0m    \033[1;36m│\033[0m\n", ms)
			winningMethod = m.id
			connectedNow = true
			break
		}
		fmt.Printf("\033[1;33m[Pending / Queued]\033[0m      \033[1;36m│\033[0m\n")
	}

	if !connectedNow {
		fmt.Println("\033[1;36m│\033[0m \033[1;33m» Direct dial blocked. Starting background 120-150s Survival Loop\033[0m\033[1;36m│\033[0m")
		fmt.Printf("\033[1;36m│\033[0m \033[1;32m» Also listening on :%s, :%s, :%s for Master Rendezvous!\033[0m   \033[1;36m│\033[0m\n", l4, dir, wss)
	}
	fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────────┘\033[0m")
	saveEnrollmentConfig(masterSpec, token, nodeName, winningMethod, l4, dir, wss)
}

func saveEnrollmentConfig(masterSpec, token, nodeName, activeMethod, l4, dir, wss string) {
	_ = os.MkdirAll("/opt/litetun", 0755)
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "6001"
	}
	content := fmt.Sprintf("PANEL_PORT=%s\nPANEL_PATH=sentinel\nPANEL_USER=admin\nPANEL_PASS=admin\nMASTER_ADDR=%s\nENROLL_TOKEN=%s\nNODE_NAME=%s\nACTIVE_METHOD=%s\nCTRL_L4_PORT=%s\nCTRL_DIRECT_PORT=%s\nCTRL_WSS_PORT=%s\n",
		port, masterSpec, token, nodeName, activeMethod, l4, dir, wss)
	_ = os.WriteFile(confPath, []byte(content), 0644)
}

func startSentinelSurvivalLoop() {
	masterSpec := strings.TrimSpace(os.Getenv("MASTER_ADDR"))
	token := strings.TrimSpace(os.Getenv("ENROLL_TOKEN"))
	nodeName := strings.TrimSpace(os.Getenv("NODE_NAME"))
	activeMethod := strings.TrimSpace(os.Getenv("ACTIVE_METHOD"))
	if masterSpec == "" || token == "" {
		return
	}
	if activeMethod == "" {
		activeMethod = "wss"
	}
	l4, dir, wss := getControlPorts()
	addLog(fmt.Sprintf("SURVIVAL: Reverse Loop (120-150s) -> %s | Listening on :%s/:%s/:%s", masterSpec, l4, dir, wss))

	order := []string{"wss", "direct", "forward"}
	appliedExits := make(map[string]string)
	lastUpgradeCheck := time.Now()

	for {
		if activeMethod != "wss" && time.Since(lastUpgradeCheck) >= 12*time.Hour {
			lastUpgradeCheck = time.Now()
			for _, cand := range order {
				if cand == activeMethod {
					break
				}
				if rUp, _, errUp := tryDialControl(masterSpec, token, nodeName, cand, false); errUp == nil && rUp.OK {
					activeMethod = cand
					saveEnrollmentConfig(masterSpec, token, nodeName, activeMethod, l4, dir, wss)
					addLog(fmt.Sprintf("SURVIVAL-UPGRADE: Upgraded Control Highway to %s!", strings.ToUpper(activeMethod)))
					break
				}
			}
		}

		rep, _, err := tryDialControl(masterSpec, token, nodeName, activeMethod, false)
		if err != nil || !rep.OK {
			for _, cand := range order {
				if r2, _, err2 := tryDialControl(masterSpec, token, nodeName, cand, false); err2 == nil && r2.OK {
					activeMethod = cand
					rep = r2
					err = nil
					saveEnrollmentConfig(masterSpec, token, nodeName, activeMethod, l4, dir, wss)
					addLog(fmt.Sprintf("SURVIVAL-OK: Connected via %s!", strings.ToUpper(activeMethod)))
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

		jitterBig, _ := rand.Int(rand.Reader, big.NewInt(30))
		time.Sleep(time.Duration(120+int(jitterBig.Int64())) * time.Second)
	}
}
