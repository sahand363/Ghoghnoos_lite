package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

type Tunnel struct {
	ID         string `json:"id"`
	ListenPort string `json:"listen_port"`
	TargetAddr string `json:"target_addr"`
	Status     string `json:"status"`
}

type ConfigReq struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

var (
	tunnels = []Tunnel{}
	logs    = []string{"[SYSTEM] LiteTun Panel running smoothly."}
	mu      sync.Mutex
)

func addLog(msg string) {
	mu.Lock()
	defer mu.Unlock()
	timestamp := time.Now().Format("15:04:05")
	logs = append([]string{fmt.Sprintf("[%s] %s", timestamp, msg)}, logs...)
	if len(logs) > 50 {
		logs = logs[:50]
	}
}

func startTCPTunnel(t Tunnel) {
	listener, err := net.Listen("tcp", ":"+t.ListenPort)
	if err != nil {
		addLog(fmt.Sprintf("ERR: Cannot bind port %s: %v", t.ListenPort, err))
		return
	}
	addLog(fmt.Sprintf("ACTIVE: Local :%s -> Remote %s", t.ListenPort, t.TargetAddr))

	for {
		client, err := listener.Accept()
		if err != nil {
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			target, err := net.DialTimeout("tcp", t.TargetAddr, 5*time.Second)
			if err != nil {
				return
			}
			defer target.Close()
			go io.Copy(target, c)
			io.Copy(c, target)
		}(client)
	}
}

func updateConfigFile(newPort, newPath string) error {
	confPath := "/opt/litetun/config.env"
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "PANEL_PORT=") {
			lines[i] = "PANEL_PORT=" + newPort
		}
		if strings.HasPrefix(line, "PANEL_PATH=") {
			lines[i] = "PANEL_PATH=" + strings.Trim(newPath, "/")
		}
	}
	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
}

func main() {
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "8880"
	}

	panelPath := strings.Trim(os.Getenv("PANEL_PATH"), "/")

	// Web UI Handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.Trim(r.URL.Path, "/")
		if panelPath != "" && reqPath != "" && reqPath != panelPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(indexHTML)
	})

	// State API
	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ram_mb":     m.Alloc / 1024 / 1024,
			"tunnels":    tunnels,
			"logs":       logs,
			"panel_port": port,
			"panel_path": panelPath,
		})
	})

	// Tunnel Creator API
	http.HandleFunc("/api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var t Tunnel
		json.NewDecoder(r.Body).Decode(&t)
		t.ID = fmt.Sprintf("tun-%d", len(tunnels)+1)
		t.Status = "Online"

		mu.Lock()
		tunnels = append(tunnels, t)
		mu.Unlock()

		go startTCPTunnel(t)
		w.WriteHeader(http.StatusOK)
	})

	// Panel Port & URL Config API
	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req ConfigReq
		json.NewDecoder(r.Body).Decode(&req)
		if req.Port == "" {
			req.Port = port
		}
		if err := updateConfigFile(req.Port, req.Path); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		addLog(fmt.Sprintf("SYSTEM: Config updated (Port: %s, Path: /%s/). Restarting...", req.Port, req.Path))
		w.WriteHeader(http.StatusOK)

		// Exit smoothly; systemd will restart the service with the new config immediately
		go func() {
			time.Sleep(1 * time.Second)
			os.Exit(0)
		}()
	})

	fmt.Printf("LiteTun running on port %s (Path: /%s/)\n", port, panelPath)

	if _, err := os.Stat("/opt/litetun/server.crt"); err == nil {
		http.ListenAndServeTLS(":"+port, "/opt/litetun/server.crt", "/opt/litetun/server.key", nil)
	} else {
		http.ListenAndServe(":"+port, nil)
	}
}
