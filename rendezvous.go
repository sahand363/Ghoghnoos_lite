package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

var activeProbeCancels = make(map[string]chan struct{})

func handleServerProbeRoute(w http.ResponseWriter, r *http.Request, panelPath string) {
	if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
		return
	}
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IP   string `json:"ip"`
		Stop bool   `json:"stop"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.IP = strings.TrimSpace(req.IP)

	mu.Lock()
	var srv *ServerNode
	if req.ID != "" {
		srv = findServerByID(req.ID)
	}
	if srv == nil && req.IP != "" {
		for i := range db.Servers {
			if db.Servers[i].IP == req.IP {
				srv = &db.Servers[i]
				break
			}
		}
	}
	if srv == nil && req.IP != "" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = fmt.Sprintf("Server-%d", len(db.Servers)+1)
		}
		tok := db.EnrollToken
		if len(db.ValidTokens) > 0 {
			tok = db.ValidTokens[len(db.ValidTokens)-1]
		}
		newNode := ServerNode{
			ID:        fmt.Sprintf("srv-%d", time.Now().Unix()%10000),
			Name:      name,
			IP:        req.IP,
			PanelPort: "6001",
			AuthToken: tok,
			Status:    "Probing...",
			Probing:   true,
		}
		db.Servers = append(db.Servers, newNode)
		srv = &db.Servers[len(db.Servers)-1]
	}
	if srv == nil {
		mu.Unlock()
		http.Error(w, "Server IP required", 400)
		return
	}

	if req.Stop {
		srv.Probing = false
		if srv.Status == "Probing..." {
			srv.Status = "Paused"
		}
		if ch, ok := activeProbeCancels[srv.ID]; ok {
			close(ch)
			delete(activeProbeCancels, srv.ID)
		}
		sName := srv.Name
		saveDBLocked()
		mu.Unlock()
		addLog(fmt.Sprintf("RENDEZVOUS-STOP [%s]: Forward prober paused by user.", sName))
		w.WriteHeader(http.StatusOK)
		return
	}

	srv.Probing = true
	if srv.Status != "Online" {
		srv.Status = "Probing..."
	}
	sCopy := *srv
	saveDBLocked()
	mu.Unlock()

	startMasterForwardProber(sCopy.ID)
	w.WriteHeader(http.StatusOK)
}

func getCandidateProbeTokensLocked(srv *ServerNode) []string {
	var list []string
	seen := make(map[string]bool)
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			list = append(list, t)
		}
	}
	if srv != nil && srv.AuthToken != "" {
		add(srv.AuthToken)
	}
	for i := len(db.ValidTokens) - 1; i >= 0; i-- {
		add(db.ValidTokens[i])
	}
	add(db.EnrollToken)
	return list
}

func startMasterForwardProber(srvID string) {
	mu.Lock()
	if oldCh, ok := activeProbeCancels[srvID]; ok {
		close(oldCh)
	}
	stopCh := make(chan struct{})
	activeProbeCancels[srvID] = stopCh
	mu.Unlock()

	go func() {
		activeMethod := ""
		lastUpgrade := time.Now()

		for {
			select {
			case <-stopCh:
				return
			default:
			}

			mu.Lock()
			srv := findServerByID(srvID)
			if srv == nil || (!srv.Probing && !strings.HasPrefix(srv.ViaMethod, "FWD-")) {
				delete(activeProbeCancels, srvID)
				mu.Unlock()
				return
			}
			ip := srv.IP
			sName := srv.Name
			candidateTokens := getCandidateProbeTokensLocked(srv)
			cMethods := db.CtrlMethods
			var queuedCmds []string
			if q, ok := pendingRemoteCmds[srvID]; ok && len(q) > 0 {
				queuedCmds = append([]string{}, q...)
				delete(pendingRemoteCmds, srvID)
			}
			if cMethods != "" {
				queuedCmds = append(queuedCmds, "set-methods:"+cMethods)
			}
			var exitsForSrv []Tunnel
			for _, t := range db.Tunnels {
				if t.TargetServer == srvID && t.Role != "server" {
					exitsForSrv = append(exitsForSrv, Tunnel{
						ID: t.ID + "-exit", Name: t.Name + " (Exit)", Role: "server", Method: t.Method,
						TunnelPort: t.TunnelPort, InboundPorts: t.InboundPorts, SNI: t.SNI,
						Fingerprint: t.Fingerprint, FragSize: t.FragSize, SpeedLimit: t.SpeedLimit, WSPath: t.WSPath, Status: "Online",
					})
				}
			}
			mu.Unlock()

			order := getAllowedControlMethods()
			if activeMethod != "" && !isControlMethodAllowed(activeMethod) {
				activeMethod = ""
			}

			l4, dir, wss, grpcP := getControlPorts()
			targetSpec := fmt.Sprintf("%s:%s:%s:%s:%s", ip, l4, dir, wss, grpcP)
			cmdPayload := strings.Join(queuedCmds, "||")

			dialWithTokens := func(m string) (EnrollReply, int64, string, error) {
				var lastErr error
				for _, tk := range candidateTokens {
					rep, ms, err := tryDialControl(targetSpec, tk, cmdPayload, m, true, exitsForSrv)
					if err == nil && rep.OK {
						return rep, ms, tk, nil
					}
					lastErr = err
				}
				return EnrollReply{}, 0, "", lastErr
			}

			if activeMethod != "" && len(order) > 0 && activeMethod != order[0] && time.Since(lastUpgrade) >= 12*time.Hour {
				lastUpgrade = time.Now()
				for _, cand := range order {
					if cand == activeMethod {
						break
					}
					if rep, _, matchedTok, err := dialWithTokens(cand); err == nil && rep.OK {
						activeMethod = cand
						applyForwardProbeSuccess(srvID, cand, matchedTok, rep)
						addLog(fmt.Sprintf("RENDEZVOUS-UPGRADE [%s]: Upgraded Forward Link to FWD-%s!", sName, strings.ToUpper(cand)))
						break
					}
				}
			}

			connected := false
			if activeMethod != "" {
				if rep, _, matchedTok, err := dialWithTokens(activeMethod); err == nil && rep.OK {
					connected = true
					applyForwardProbeSuccess(srvID, activeMethod, matchedTok, rep)
				}
			}

			if !connected {
				addLog(fmt.Sprintf("RENDEZVOUS-HUNT [%s]: Probing %s via [%s]...", sName, ip, strings.ToUpper(strings.Join(order, " ➔ "))))
				for _, cand := range order {
					if rep, ms, matchedTok, err := dialWithTokens(cand); err == nil && rep.OK {
						activeMethod = cand
						connected = true
						applyForwardProbeSuccess(srvID, cand, matchedTok, rep)
						addLog(fmt.Sprintf("RENDEZVOUS-OK [%s]: Connected to %s via FWD-%s (%d ms)!", sName, ip, strings.ToUpper(cand), ms))
						break
					}
				}
			}

			if !connected {
				addLog(fmt.Sprintf("RENDEZVOUS-WAIT [%s]: %s unreachable. Retrying in 120-150s...", sName, ip))
			}

			jitterBig, _ := rand.Int(rand.Reader, big.NewInt(30))
			sleepDur := time.Duration(120+int(jitterBig.Int64())) * time.Second
			select {
			case <-stopCh:
				return
			case <-time.After(sleepDur):
			}
		}
	}()
}

func applyForwardProbeSuccess(srvID, method, matchedTok string, rep EnrollReply) {
	mu.Lock()
	srv := findServerByID(srvID)
	if srv == nil {
		mu.Unlock()
		return
	}
	if rep.PanelPort != "" {
		srv.PanelPort = rep.PanelPort
	}
	if rep.Version != "" {
		srv.Version = rep.Version
	}
	if matchedTok != "" {
		srv.AuthToken = matchedTok
	}
	if rep.MasterPin != "" {
		srv.CertPin = rep.MasterPin
		syncServerTunnelsPinLocked(srvID, rep.MasterPin)
	}
	srv.Status = "Online"
	srv.Probing = false
	srv.ViaMethod = "FWD-" + strings.ToUpper(method)
	srv.LastSeen = time.Now().Unix()

	remoteStateCache[srvID] = EnrollHeartbeat{
		IP:         srv.IP,
		PanelPort:  srv.PanelPort,
		CertPin:    srv.CertPin,
		Version:    srv.Version,
		ServerTime: rep.ServerTime,
		Metrics:    rep.Metrics,
		Logs:       rep.Logs,
	}
	saveDBLocked()
	mu.Unlock()
}
