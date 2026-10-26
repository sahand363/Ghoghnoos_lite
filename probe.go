package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// Measures pure 1-RTT tunnel payload latency after TCP/TLS connection is established
func probeTunnelOnce(tun Tunnel) PingPoint {
	ports := parsePortList(tun.InboundPorts)
	firstPort := "2053"
	if len(ports) > 0 {
		firstPort = ports[0]
	}
	pt := PingPoint{Timestamp: time.Now().Unix(), TunnelID: tun.ID, ServerID: tun.TargetServer}

	if tun.Method == "forward" {
		c, err := net.DialTimeout("tcp", tun.RemoteAddr, 4*time.Second)
		if err != nil {
			pt.OK = false
			pt.Error = "Tunnel Port Closed"
			recordPingPoint(pt)
			return pt
		}
		defer c.Close()

		rttStart := time.Now()
		c.Write([]byte("FWD|probe:" + firstPort + "\n"))
		reply, err := bufio.NewReader(c).ReadString('\n')
		pt.LatencyMS = time.Since(rttStart).Milliseconds()
		pt.OK = (err == nil && strings.HasPrefix(reply, "OK")) || err == nil
		if !pt.OK {
			pt.Error = "Handshake Rejected"
		}
		recordPingPoint(pt)
		return pt
	}

	tlsCfg := buildTunnelTLSConfig(tun)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", tun.RemoteAddr, tlsCfg)
	if err != nil {
		pt.OK = false
		pt.Error = "Tunnel Offline"
		recordPingPoint(pt)
		return pt
	}
	defer conn.Close()

	// Start timer AFTER TCP + TLS handshake so only 1-RTT probe exchange is measured
	rttStart := time.Now()
	if tun.Method == "direct" || tun.Method == "spoof" {
		conn.Write([]byte("LTUN|probe:" + firstPort + "\n"))
		reply, err := bufio.NewReader(conn).ReadString('\n')
		pt.LatencyMS = time.Since(rttStart).Milliseconds()
		pt.OK = (err == nil && strings.HasPrefix(reply, "OK")) || err == nil
		if !pt.OK {
			pt.Error = "Tunnel Unresponsive"
		}
		recordPingPoint(pt)
		return pt
	}

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nX-Target-Port: probe:%s\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		tun.WSPath, tun.SNI, firstPort, wsKey)
	conn.Write([]byte(req))
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	pt.LatencyMS = time.Since(rttStart).Milliseconds()
	pt.OK = err == nil && strings.Contains(statusLine, "101")
	if !pt.OK {
		pt.Error = "Upgrade Rejected"
	}
	recordPingPoint(pt)
	return pt
}

// Ultra-relaxed background probe: runs every ~10 to 12 minutes (600s to 720s with jitter)
func startPingMonitor() {
	for {
		jitterBig, _ := rand.Int(rand.Reader, big.NewInt(120))
		sleepSec := 600 + int(jitterBig.Int64()) // 10 to 12 minutes
		time.Sleep(time.Duration(sleepSec) * time.Second)

		mu.Lock()
		tuns := append([]Tunnel{}, db.Tunnels...)
		mu.Unlock()
		for _, t := range tuns {
			if t.Role != "server" && t.RemoteAddr != "" {
				probeTunnelOnce(t)
			}
		}
	}
}
