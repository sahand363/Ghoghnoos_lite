package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

//go:embed index.html
var ui embed.FS

type Tunnel struct {
	ID         string `json:"id"`
	ListenPort string `json:"listen_port"`
	TargetAddr string `json:"target_addr"`
	Status     string `json:"status"`
}

var (
	tunnels = []Tunnel{}
	logs    = []string{"[SYSTEM] Ghoghnoos Lite initialized successfully."}
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
		addLog(fmt.Sprintf("ERR: Failed to bind port %s: %v", t.ListenPort, err))
		return
	}
	addLog(fmt.Sprintf("TUNNEL: Listening on :%s -> Forwarding to %s", t.ListenPort, t.TargetAddr))

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

func main() {
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "8080"
	}

	panelPath := os.Getenv("PANEL_PATH")
	prefix := "/"
	if panelPath != "" {
		prefix = "/" + panelPath + "/"
	}

	// Serve the embedded HTML file both at root and at the secret PANEL_PATH
	http.Handle("/", http.FileServer(http.FS(ui)))
	if prefix != "/" {
		http.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.FS(ui))))
	}

	// API: Server Stats & Logs
	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ram_mb":  m.Alloc / 1024 / 1024,
			"tunnels": tunnels,
			"logs":    logs,
		})
	})

	// API: Create a new Tunnel
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

	fmt.Printf("Ghoghnoos Panel running on port %s (Path: %s)\n", port, prefix)

	// Automatically use SSL if enabled via Menu Option 8
	if _, err := os.Stat("server.crt"); err == nil {
		http.ListenAndServeTLS(":"+port, "server.crt", "server.key", nil)
	} else {
		http.ListenAndServe(":"+port, nil)
	}
}
