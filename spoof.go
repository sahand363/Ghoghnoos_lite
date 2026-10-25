package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// Low-level packet fragmenter: splits outgoing TLS ClientHello records into small TCP segments to defeat DPI
type fragmentConn struct {
	net.Conn
	fragSize   int
	packetsOut int
}

func (f *fragmentConn) Write(b []byte) (int, error) {
	f.packetsOut++
	if f.fragSize <= 0 || f.packetsOut > 3 || len(b) <= f.fragSize {
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
		time.Sleep(2 * time.Millisecond)
	}
	return total, nil
}

// Crafts a real binary RFC-5246 TLS ClientHello packet carrying the decoy/whitelisted SNI
func buildFakeClientHello(fakeSNI string) []byte {
	if fakeSNI == "" {
		fakeSNI = "ebank.bmi.ir"
	}
	sniBytes := []byte(fakeSNI)
	var ext bytes.Buffer
	// Extension: server_name (0x0000)
	binary.Write(&ext, binary.BigEndian, uint16(0x0000))
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)+5))
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)+3))
	ext.WriteByte(0x00)
	binary.Write(&ext, binary.BigEndian, uint16(len(sniBytes)))
	ext.Write(sniBytes)

	var ch bytes.Buffer
	ch.Write([]byte{0x03, 0x03}) // TLS 1.2 record version
	rnd := make([]byte, 32)
	rand.Read(rnd)
	ch.Write(rnd)
	ch.WriteByte(0x00) // SessionID len 0
	// Ciphers: 0x1301 (TLS_AES_128_GCM_SHA256), 0xCCA8 (TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305), 0x0100 (Compression)
	ch.Write([]byte{0x00, 0x04, 0x13, 0x01, 0xcc, 0xa8, 0x01, 0x00})
	binary.Write(&ch, binary.BigEndian, uint16(ext.Len()))
	ch.Write(ext.Bytes())

	var handshake bytes.Buffer
	handshake.WriteByte(0x01) // ClientHello type
	l := ch.Len()
	handshake.Write([]byte{byte(l >> 16), byte(l >> 8), byte(l)})
	handshake.Write(ch.Bytes())

	var record bytes.Buffer
	record.Write([]byte("SNISPOOF")) // 8-byte sync magic for Exit Sentinel
	binary.Write(&record, binary.BigEndian, uint16(handshake.Len()))
	record.Write(handshake.Bytes())
	return record.Bytes()
}

func buildSpoofTLSConfig(t Tunnel) *tls.Config {
	pinClean := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(t.CertPin, ":", "")))
	cfg := &tls.Config{
		ServerName:         t.SNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	switch strings.ToLower(t.Fingerprint) {
	case "firefox":
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	case "safari":
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	default: // Chrome 120
		cfg.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
	}
	if pinClean != "" {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pinClean {
				addLog(fmt.Sprintf("SEC-ALERT [%s]: Cert Pin mismatch!", t.Name))
				return fmt.Errorf("certificate pin mismatch")
			}
			return nil
		}
	}
	return cfg
}
