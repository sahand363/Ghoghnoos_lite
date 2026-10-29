package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

//go:embed i18n.js
var i18nJS []byte

//go:embed app.js
var appJS []byte

const (
	appVersion = "v2.6.0"
	wsGUID     = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	dbPath     = "/opt/ghoghnoos/panel_db.json"
	logPath    = "/opt/ghoghnoos/panel.log"
	confPath   = "/opt/ghoghnoos/config.env"
	certFile   = "/opt/ghoghnoos/cert.pem"
	keyFile    = "/opt/ghoghnoos/key.pem"
)

var (
	db = Database{
		Servers:     []ServerNode{},
		Tunnels:     []Tunnel{},
		PingHistory: []PingPoint{},
		DailyUsage:  []DailyBW{},
		Prefs: UIPrefs{
			Theme:          "cyan",
			Lang:           "en",
			ActiveTab:      "servers",
			SelectedServer: "local",
		},
	}
	activeListeners   = make(map[string][]net.Listener)
	remoteStateCache  = make(map[string]EnrollHeartbeat)
	pendingRemoteCmds = make(map[string][]string)
	logs              = []string{}
	localCert         tls.Certificate
	localPinHex       string
	localIP           string
	panelUser         string
	panelPass         string
	appRxBytes        uint64
	appTxBytes        uint64
	sysMetrics        SystemMetrics
	mu                sync.Mutex
)

func loadEnvFile() {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}

func initSelfSignedCert() {
	_ = os.MkdirAll("/opt/ghoghnoos", 0755)
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil && len(cert.Certificate) > 0 {
		sum := sha256.Sum256(cert.Certificate[0])
		localPinHex = hex.EncodeToString(sum[:])
		localCert = cert
		return
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "cloudflare-dns.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"cloudflare-dns.com", "localhost"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err == nil {
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
		_ = os.WriteFile(certFile, certPEM, 0600)
		_ = os.WriteFile(keyFile, keyPEM, 0600)
	}
	sum := sha256.Sum256(derBytes)
	localPinHex = hex.EncodeToString(sum[:])
	localCert = tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
}

func main() {
	loadEnvFile()
	initSessionSecret()

	if len(os.Args) >= 4 && os.Args[1] == "--enroll" {
		nodeName := ""
		if len(os.Args) >= 5 {
			nodeName = os.Args[4]
		}
		runCLIEnrollmentMatrix(os.Args[2], os.Args[3], nodeName)
		return
	}

	port := os.Getenv("PANEL_PORT")
	if port == "" || port == "8880" {
		port = "6001"
		if data, err := os.ReadFile(confPath); err == nil {
			updated := strings.ReplaceAll(string(data), "PANEL_PORT=8880", "PANEL_PORT=6001")
			_ = os.WriteFile(confPath, []byte(updated), 0644)
		}
	}
	panelPath := strings.Trim(os.Getenv("PANEL_PATH"), "/")
	panelUser = strings.TrimSpace(os.Getenv("PANEL_USER"))
	panelPass = strings.TrimSpace(os.Getenv("PANEL_PASS"))

	loadLogsFromDisk()
	initSelfSignedCert()
	loadDBFromDisk(port)
	ensureEnrollToken()
	startControlListeners()
	go startTelemetryMonitor()
	go startPingMonitor()
	go startSentinelSurvivalLoop()
	go startDailyUpdateChecker()
	registerRoutes(port, panelPath)

	fmt.Printf("Ghoghnoos Tunnel %s running on port %s (Path: /%s/ | Auth: %v)\n", appVersion, port, panelPath, panelUser != "")
	if _, err := os.Stat("/opt/ghoghnoos/server.crt"); err == nil {
		http.ListenAndServeTLS(":"+port, "/opt/ghoghnoos/server.crt", "/opt/ghoghnoos/server.key", nil)
	} else {
		http.ListenAndServe(":"+port, nil)
	}
}
