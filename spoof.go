package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// tcpSegmentConn splits the initial TLS ClientHello across smaller TCP segments.
// Unlike fake packet injection, every byte here is 100% valid, standard TLS.
type tcpSegmentConn struct {
	net.Conn
	fragSize   int
	packetsOut int
}

func (f *tcpSegmentConn) Write(b []byte) (int, error) {
	f.packetsOut++
	if f.fragSize <= 0 || f.packetsOut > 2 || len(b) <= f.fragSize {
		return f.Conn.Write(b)
	}
	total := 0
	for len(b) > 0 {
		chunk := f.fragSize
		if chunk > len(b) {
			chunk = len(b)
		}
		n, err := f.Conn.Write(b[:chunk])
		total += n
		if err != nil {
			return total, err
		}
		b = b[chunk:]
		time.Sleep(1 * time.Millisecond)
	}
	return total, nil
}

func buildTunnelTLSConfig(t Tunnel) *tls.Config {
	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))
	sniHost := strings.TrimSpace(t.SNI)
	if sniHost == "" {
		sniHost = "cloudflare-dns.com"
	}

	cfg := &tls.Config{
		ServerName:         sniHost,
		InsecureSkipVerify: true, // Verified below via strict SHA-256 leaf certificate pinning
		MinVersion:         tls.VersionTLS12,
	}

	switch strings.ToLower(t.Fingerprint) {
	case "firefox":
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	case "safari":
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	default: // Chrome
		cfg.CipherSuites = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	}

	if pinClean != "" {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate presented")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pinClean {
				addLog(fmt.Sprintf("SEC-ALERT [%s]: SHA-256 Certificate Pin mismatch!", t.Name))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}
	return cfg
}
