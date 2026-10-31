package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

var (
	activeH3Servers    = make(map[string]*http3.Server)
	activeH3Transports = make(map[string]*http3.Transport)
	h3Mu               sync.Mutex
)

type h3FlushWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (fw *h3FlushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if n > 0 && fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}

func buildH3QUICConfig() *quic.Config {
	return &quic.Config{
		MaxIdleTimeout:                 60 * time.Second,
		KeepAlivePeriod:                15 * time.Second,
		MaxIncomingStreams:             4096,
		MaxIncomingUniStreams:          1024,
		InitialStreamReceiveWindow:     6 * 1024 * 1024,
		MaxStreamReceiveWindow:         16 * 1024 * 1024,
		InitialConnectionReceiveWindow: 15 * 1024 * 1024,
		MaxConnectionReceiveWindow:     32 * 1024 * 1024,
		Allow0RTT:                      false,
	}
}

func stopH3TunnelIfRunning(id string) {
	baseID := strings.TrimSuffix(id, "-exit")
	h3Mu.Lock()
	if srv, ok := activeH3Servers[id]; ok {
		_ = srv.Close()
		delete(activeH3Servers, id)
	}
	if tr, ok := activeH3Transports[id]; ok {
		_ = tr.Close()
		delete(activeH3Transports, id)
	}
	if tr, ok := activeH3Transports[baseID]; ok {
		_ = tr.Close()
		delete(activeH3Transports, baseID)
	}
	h3Mu.Unlock()
}

func getOrCreateH3Transport(t Tunnel) *http3.Transport {
	h3Mu.Lock()
	defer h3Mu.Unlock()
	if tr, ok := activeH3Transports[t.ID]; ok {
		return tr
	}
	tlsCfg := buildTunnelTLSConfig(t)
	tlsCfg.NextProtos = []string{http3.NextProtoH3}

	tr := &http3.Transport{
		TLSClientConfig:    tlsCfg,
		QUICConfig:         buildH3QUICConfig(),
		DisableCompression: true,
	}
	activeH3Transports[t.ID] = tr
	return tr
}

func normalizeH3Path(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "/cdn-cgi/trace"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// Engine 2: Real HTTP/3 (RFC 9114) Multiplexed Stream Engine
func startH3Engine(t Tunnel) {
	stopTunnelIfRunning(t.ID)
	stopH3TunnelIfRunning(t.ID)

	inboundPorts := parsePortList(t.InboundPorts)
	if len(inboundPorts) == 0 {
		inboundPorts = []string{"2053"}
	}
	tunPort := strings.TrimSpace(t.TunnelPort)
	if tunPort == "" {
		tunPort = "11350"
	}
	h3Path := normalizeH3Path(t.WSPath)

	// EXIT NODE: Real HTTP/3 Server over QUIC/UDP
	if t.Role == "server" {
		tlsCfg := http3.ConfigureTLSConfig(&tls.Config{
			Certificates: []tls.Certificate{localCert},
			MinVersion:   tls.VersionTLS13,
		})

		mux := http.NewServeMux()
		mux.HandleFunc(h3Path, func(w http.ResponseWriter, r *http.Request) {
			handleH3ServerStream(w, r, t, inboundPorts[0])
		})
		// Fallback handler: Active DPI scanners probing root over HTTP/3 get a normal 200 OK web response
		if h3Path != "/" {
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Target-Port") != "" {
					handleH3ServerStream(w, r, t, inboundPorts[0])
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Server", "cloudflare")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","protocol":"h3"}`))
			})
		}

		h3Srv := &http3.Server{
			Addr:       ":" + tunPort,
			TLSConfig:  tlsCfg,
			QUICConfig: buildH3QUICConfig(),
			Handler:    mux,
		}

		udpAddr, err := net.ResolveUDPAddr("udp", ":"+tunPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [H3-Exit %s]: Invalid UDP port :%s -> %v", t.Name, tunPort, err))
			return
		}
		udpConn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			addLog(fmt.Sprintf("ERR [H3-Exit %s]: Cannot bind UDP :%s -> %v", t.Name, tunPort, err))
			return
		}

		h3Mu.Lock()
		activeH3Servers[t.ID] = h3Srv
		h3Mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [H3-Exit %s]: UDP :%s (ALPN h3) -> 127.0.0.1:[%s]", t.Name, tunPort, strings.Join(inboundPorts, ",")))
		go func() {
			defer udpConn.Close()
			_ = h3Srv.Serve(udpConn)
		}()
		return
	}

	// ENTRY NODE: Bind local TCP Inbound Ports and multiplex over pooled HTTP/3 Transport
	_ = getOrCreateH3Transport(t)
	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [H3-Entry %s]: Cannot bind TCP :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [H3-Entry %s]: TCP :%s -> H3 https://%s%s (SNI: %s)", t.Name, iPort, t.RemoteAddr, h3Path, t.SNI))

		go func(l net.Listener, targetPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handleH3ClientConn(client, t, targetPort, h3Path)
			}
		}(ln, iPort)
	}
}

func handleH3ClientConn(client net.Conn, t Tunnel, targetPort, h3Path string) {
	defer client.Close()

	tr := getOrCreateH3Transport(t)
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	urlStr := fmt.Sprintf("https://%s%s", t.RemoteAddr, h3Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, pr)
	if err != nil {
		return
	}
	if sni := strings.TrimSpace(t.SNI); sni != "" {
		req.Host = sni
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("X-Target-Port", targetPort)

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(pw, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	// Stream client -> HTTP/3 Request DATA frames immediately
	go func() {
		_, _ = io.Copy(txW, client)
		_ = pw.Close()
	}()

	resp, err := tr.RoundTrip(req)
	if err != nil {
		addLog(fmt.Sprintf("ERR [H3 %s]: RoundTrip to %s failed: %v", t.Name, t.RemoteAddr, err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		addLog(fmt.Sprintf("ERR [H3 %s]: Exit returned HTTP %d", t.Name, resp.StatusCode))
		return
	}

	// Stream HTTP/3 Response DATA frames -> client
	_, _ = io.Copy(rxW, resp.Body)
}

func handleH3ServerStream(w http.ResponseWriter, r *http.Request, t Tunnel, defaultPort string) {
	defer r.Body.Close()

	targetPort := strings.TrimSpace(r.Header.Get("X-Target-Port"))
	if targetPort == "" {
		targetPort = defaultPort
	}

	// Handle end-to-end latency & port reachability probe
	if strings.HasPrefix(targetPort, "probe:") {
		probePort := strings.TrimPrefix(targetPort, "probe:")
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+probePort, 2*time.Second); err == nil {
			c.Close()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK\n"))
		} else {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("ERR:X-UI Port Refused\n"))
		}
		return
	}

	target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("X-UI ERR [H3 %s]: Local 127.0.0.1:%s refused", t.Name, targetPort))
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer target.Close()
	enableKeepAlive(target)

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	mbLimit := parseSpeedLimit(t.SpeedLimit)
	txW := newThrottledWriter(target, &appRxBytes, mbLimit)
	rxW := newThrottledWriter(&h3FlushWriter{w: w, flusher: flusher}, &appTxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(txW, r.Body); done <- struct{}{} }()
	go func() { _, _ = io.Copy(rxW, target); done <- struct{}{} }()
	<-done
}

// End-to-End Ping Probe over Real HTTP/3
func probeH3Tunnel(t Tunnel, firstInboundPort string) (int64, error) {
	start := time.Now()
	tr := getOrCreateH3Transport(t)
	h3Path := normalizeH3Path(t.WSPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s%s", t.RemoteAddr, h3Path), http.NoBody)
	if err != nil {
		return 0, err
	}
	if sni := strings.TrimSpace(t.SNI); sni != "" {
		req.Host = sni
	}
	req.Header.Set("X-Target-Port", "probe:"+firstInboundPort)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		return time.Since(start).Milliseconds(), err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "OK") {
		return time.Since(start).Milliseconds(), fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return time.Since(start).Milliseconds(), nil
}

// Engine 1: Real HTTP/3 Control Listener & Dialer (Optional for Control Plane)
func startH3ControlListener(port string) {
	tlsCfg := http3.ConfigureTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{localCert},
		MinVersion:   tls.VersionTLS13,
	})
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
		hb.ViaMethod = "H3"
		rep := processIncomingEnroll(hb)
		if !rep.OK {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
	})
	srv := &http3.Server{Addr: ":" + port, TLSConfig: tlsCfg, QUICConfig: buildH3QUICConfig(), Handler: mux}
	addLog(fmt.Sprintf("CONTROL-READY: HTTP/3 (RFC 9114) Ingress listening on UDP :%s", port))
	_ = srv.ListenAndServe()
}
