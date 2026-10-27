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
		newNode := ServerNode{
			ID:        fmt.Sprintf("srv-%d", time.Now().Unix()%10000),
			Name:      name,
			IP:        req.IP,
			SSHPort:   "22",
			SSHUser:   "root",
			PanelPort: "6001",
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
			tok := db.EnrollToken
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

			l4, dir, wss := getControlPorts()
			targetSpec := fmt.Sprintf("%s:%s:%s:%s", ip, l4, dir, wss)
			cmdPayload := strings.Join(queuedCmds, "||")

			if activeMethod != "" && activeMethod != "wss" && isControlMethodAllowed("wss") && time.Since(lastUpgrade) >= 12*time.Hour {
				lastUpgrade = time.Now()
				for _, cand := range order {
					if cand == activeMethod {
						break
					}
					if rep, _, err := tryDialControl(targetSpec, tok, cmdPayload, cand, true, exitsForSrv); err == nil && rep.OK {
						activeMethod = cand
						applyForwardProbeSuccess(srvID, cand, rep)
						addLog(fmt.Sprintf("RENDEZVOUS-UPGRADE [%s]: Upgraded Forward Link to FWD-%s!", sName, strings.ToUpper(cand)))
						break
					}
				}
			}

			connected := false
			if activeMethod != "" {
				if rep, _, err := tryDialControl(targetSpec, tok, cmdPayload, activeMethod, true, exitsForSrv); err == nil && rep.OK {
					connected = true
					applyForwardProbeSuccess(srvID, activeMethod, rep)
				}
			}

			if !connected {
				addLog(fmt.Sprintf("RENDEZVOUS-HUNT [%s]: Probing %s via [%s]...", sName, ip, strings.ToUpper(strings.Join(order, " ➔ "))))
				for _, cand := range order {
					if rep, ms, err := tryDialControl(targetSpec, tok, cmdPayload, cand, true, exitsForSrv); err == nil && rep.OK {
						activeMethod = cand
						connected = true
						applyForwardProbeSuccess(srvID, cand, rep)
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

func applyForwardProbeSuccess(srvID, method string, rep EnrollReply) {
	mu.Lock()
	srv := findServerByID(srvID)
	if srv == nil {
		mu.Unlock()
		return
	}
	if rep.PanelPort != "" {
		srv.PanelPort = rep.PanelPort
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
		ServerTime: rep.ServerTime,
		Metrics:    rep.Metrics,
		Logs:       rep.Logs,
	}
	saveDBLocked()
	mu.Unlock()
}
