package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

var activeProbers = make(map[string]chan struct{})

func registerRendezvousRoutes(panelPath string) {
	http.HandleFunc("/api/servers/probe", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		var req struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			IP   string `json:"ip"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		req.IP = strings.TrimSpace(req.IP)
		req.Name = strings.TrimSpace(req.Name)

		mu.Lock()
		var targetSrv *ServerNode
		if req.ID != "" {
			targetSrv = findServerByID(req.ID)
		}
		if targetSrv == nil && req.IP != "" {
			for i := range db.Servers {
				if db.Servers[i].IP == req.IP {
					targetSrv = &db.Servers[i]
					break
				}
			}
		}
		if targetSrv == nil {
			if req.IP == "" {
				mu.Unlock()
				http.Error(w, "Missing Server IP", http.StatusBadRequest)
				return
			}
			if req.Name == "" {
				req.Name = fmt.Sprintf("Server-%d", len(db.Servers)+1)
			}
			newNode := ServerNode{
				ID:        fmt.Sprintf("srv-%d", time.Now().Unix()%10000),
				Name:      req.Name,
				IP:        req.IP,
				SSHPort:   "22",
				SSHUser:   "root",
				PanelPort: "6001",
				Status:    "Probing...",
				Probing:   true,
			}
			db.Servers = append(db.Servers, newNode)
			targetSrv = &db.Servers[len(db.Servers)-1]
		} else {
			targetSrv.Probing = true
			if targetSrv.Status != "Online" {
				targetSrv.Status = "Probing..."
			}
		}
		srvID := targetSrv.ID
		srvName := targetSrv.Name
		srvIP := targetSrv.IP
		saveDBLocked()
		mu.Unlock()

		startMasterForwardProber(srvID, srvName, srvIP)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": srvID})
	})

	http.HandleFunc("/api/servers/probe/stop", func(w http.ResponseWriter, r *http.Request) {
		if !checkBasicAuth(w, r, panelPath) || r.Method != http.MethodPost {
			return
		}
		id := r.URL.Query().Get("id")
		stopMasterForwardProber(id)
		mu.Lock()
		if srv := findServerByID(id); srv != nil {
			srv.Probing = false
			if srv.Status == "Probing..." {
				srv.Status = "Paused"
			}
			saveDBLocked()
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func stopMasterForwardProber(srvID string) {
	mu.Lock()
	if ch, ok := activeProbers[srvID]; ok {
		close(ch)
		delete(activeProbers, srvID)
	}
	mu.Unlock()
}

func startMasterForwardProber(srvID, srvName, srvIP string) {
	stopMasterForwardProber(srvID)
	stopCh := make(chan struct{})
	mu.Lock()
	activeProbers[srvID] = stopCh
	mu.Unlock()

	go func() {
		l4, dir, wss := getControlPorts()
		addLog(fmt.Sprintf("RENDEZVOUS-START [%s]: Forward Probing %s on WSS(:%s) ➔ Direct(:%s) ➔ L4(:%s)", srvName, srvIP, wss, dir, l4))
		order := []string{"wss", "direct", "forward"}
		activeMethod := ""

		for {
			mu.Lock()
			srv := findServerByID(srvID)
			if srv == nil || !srv.Probing {
				mu.Unlock()
				return
			}
			tok := db.EnrollToken
			var queuedCmds []string
			if q, ok := pendingRemoteCmds[srvID]; ok && len(q) > 0 {
				queuedCmds = append([]string{}, q...)
				delete(pendingRemoteCmds, srvID)
			}
			mu.Unlock()

			l4, dir, wss = getControlPorts()
			targetSpec := fmt.Sprintf("%s:%s:%s:%s", srvIP, l4, dir, wss)
			cmdPayload := strings.Join(queuedCmds, "||")

			var rep EnrollReply
			var ms int64
			var err error

			if activeMethod != "" {
				rep, ms, err = tryDialControl(targetSpec, tok, cmdPayload, activeMethod, true)
			}
			if activeMethod == "" || err != nil || !rep.OK {
				for _, cand := range order {
					if r2, lat, err2 := tryDialControl(targetSpec, tok, cmdPayload, cand, true); err2 == nil && r2.OK {
						activeMethod = cand
						rep = r2
						ms = lat
						err = nil
						break
					}
				}
			}

			if err == nil && rep.OK {
				mu.Lock()
				if srv := findServerByID(srvID); srv != nil {
					wasOffline := srv.Status != "Online"
					srv.Status = "Online"
					srv.ViaMethod = "FWD-" + strings.ToUpper(activeMethod)
					if rep.MasterPin != "" {
						srv.CertPin = rep.MasterPin
					}
					if rep.PanelPort != "" {
						srv.PanelPort = rep.PanelPort
					}
					srv.LastSeen = time.Now().Unix()
					remoteStateCache[srvID] = EnrollHeartbeat{
						IP:         srvIP,
						PanelPort:  srv.PanelPort,
						CertPin:    srv.CertPin,
						ViaMethod:  srv.ViaMethod,
						ServerTime: rep.ServerTime,
						Metrics:    rep.Metrics,
						Logs:       rep.Logs,
					}
					save
