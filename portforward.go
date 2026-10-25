package main

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// startPortForwardEngine implements high-performance L4 raw TCP relaying (native socat-like stream)
func startPortForwardEngine(t Tunnel) {
	mu.Lock()
	stopTunnelIfRunning(t.ID)
	mu.Unlock()

	inboundPorts := parsePortList(t.InboundPorts)
	if len(inboundPorts) == 0 {
		inboundPorts = []string{"2053"}
	}

	remoteHost := t.RemoteAddr
	if !strings.Contains(remoteHost, ":") && t.TunnelPort != "" {
		remoteHost = net.JoinHostPort(remoteHost, t.TunnelPort)
	}

	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Forward %s]: Cannot bind Port :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [Forward %s]: :%s -> %s (Direct L4 TCP)", t.Name, iPort, remoteHost))

		go func(l net.Listener, target string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handlePortForwardConn(client, target, t.SpeedLimit)
			}
		}(ln, remoteHost)
	}
}

func handlePortForwardConn(client net.Conn, targetAddr string, speedLimit string) {
	defer client.Close()

	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	remote, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		return
	}
	defer remote.Close()
	enableKeepAlive(remote)

	mbLimit, _ := strconv.Atoi(speedLimit)
	txW := newThrottledWriter(remote, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, client); done <- struct{}{} }()
	go func() { io.Copy(rxW, remote); done <- struct{}{} }()
	<-done
}
