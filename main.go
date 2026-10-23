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

const (
	wsGUID   = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	dbPath   = "/opt/litetun/panel_db.json"
	logPath  = "/opt/litetun/panel.log"
	confPath = "/opt/litetun/config.env"
)

var (
	db = Database{
		Servers: []ServerNode{},
		Tunnels: []Tunnel{},
		Prefs: UIPrefs{
			Theme:          "amber",
			Lang:           "en",
			ActiveTab:      "servers",
			SelectedServer: "local",
		},
	}
	activeListeners = make(map[string]net.Listener)
	logs            = []string{}
	localCert       tls.Certificate
	localPinHex     string
	localIP         string
	mu              sync.Mutex
)

func initSelfSignedCert() {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "cloudflare-dns.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"cloudflare-dns.com", "localhost"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return
	}
	sum := sha256.Sum256(derBytes)
	localPinHex = hex.EncodeToString(sum[:])
	localCert = tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
}

func main() {
	port := os.Getenv("PANEL_PORT")
	if port == "" {
		port = "8880"
	}
	panelPath := strings.Trim(os.Getenv("PANEL_PATH"), "/")

	loadLogsFromDisk()
	initSelfSignedCert()
	loadDBFromDisk(port)
	registerRoutes(port, panelPath)

	fmt.Printf("LiteTun running on port %s (Path: /%s/)\n", port, panelPath)
	if _, err := os.Stat("/opt/litetun/server.crt"); err == nil {
		http.ListenAndServeTLS(":"+port, "/opt/litetun/server.crt", "/opt/litetun/server.key", nil)
	} else {
		http.ListenAndServe(":"+port, nil)
	}
}
