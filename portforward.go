package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// startPortForwardEngine implements L4 raw TCP relaying (native socat-like stream)
// Supports both Entry (Iran) -> Exit (Kharej Tunnel Port) -> Local X-UI Inbound Port
func startPortForwardEngine(t Tunnel) {
	mu.Lock()
	stopTunnelIfRunning(t.ID)
	mu.Unlock()

	inboundPorts := parsePortList(t.InboundPorts)
	if len(inboundPorts) == 0 {
		inboundPorts = []string{"2053"}
	}
	tunPort := strings.TrimSpace(t.TunnelPort)
	if tunPort == "" {
		tunPort = "10202"
	}

	// EXIT SERVER (Kharej): Listen on TunnelPort (:10202) and forward raw TCP to 127.0.0.1:<X-UI Port>
	if t.Role == "server" {
		ln, err := net.Listen("tcp", ":"+tunPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Forward-Exit %s]: Cannot bind Tunnel Port :%s -> %v", t.Name, tunPort, err))
			return
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()
		addLog(fmt.Sprintf("LISTEN [Forward-Exit %s]: :%s -> X-UI 127.0.0.1:[%s]", t.Name, tunPort, strings.Join(inboundPorts, ",")))

		go func(l net.Listener, defPort string) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(conn)
				go handlePortForwardServer(conn, t, defPort)
			}
		}(ln, inboundPorts[0])
		return
	}

	// ENTRY SERVER (Iran): Listen on each Inbound Port (:45455) and relay to Exit Server (:10202)
	remoteHost := t.RemoteAddr
	if !strings.Contains(remoteHost, ":") && tunPort != "" {
		remoteHost = net.JoinHostPort(remoteHost, tunPort)
	}

	for _, iPort := range inboundPorts {
		ln, err := net.Listen("tcp", ":"+iPort)
		if err != nil {
			addLog(fmt.Sprintf("ERR [Forward-Entry %s]: Cannot bind Port :%s -> %v", t.Name, iPort, err))
			continue
		}
		mu.Lock()
		activeListeners[t.ID] = append(activeListeners[t.ID], ln)
		mu.Unlock()

		addLog(fmt.Sprintf("LISTEN [Forward-Entry %s]: :%s -> %s (X-UI :%s)", t.Name, iPort, remoteHost, iPort))

		go func(l net.Listener, targetAddr, xuiPort string) {
			for {
				client, err := l.Accept()
				if err != nil {
					return
				}
				enableKeepAlive(client)
				go handlePortForwardClient(client, targetAddr, xuiPort, t.SpeedLimit)
			}
		}(ln, remoteHost, iPort)
	}
}

func handlePortForwardClient(client net.Conn, targetAddr, xuiPort, speedLimit string) {
	defer client.Close()

	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
	remote, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		return
	}
	defer remote.Close()
	enableKeepAlive(remote)

	// Send lightweight L4 routing header so Exit node knows which local X-UI port to dial
	if _, err := remote.Write([]byte("FWD|" + xuiPort + "\n")); err != nil {
		return
	}

	mbLimit, _ := strconv.Atoi(speedLimit)
	txW := newThrottledWriter(remote, &appTxBytes, mbLimit)
	rxW := newThrottledWriter(client, &appRxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, client); done <- struct{}{} }()
	go func() { io.Copy(rxW, remote); done <- struct{}{} }()
	<-done
}

func handlePortForwardServer(conn net.Conn, t Tunnel, defaultPort string) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	firstLine, err := br.ReadString('\n')
	if err != nil {
		return
	}

	targetPort := defaultPort
	if strings.HasPrefix(firstLine, "FWD|") {
		targetPort = strings.TrimSpace(strings.TrimPrefix(firstLine, "FWD|"))
	}

	if strings.HasPrefix(targetPort, "probe:") {
		probePort := strings.TrimPrefix(targetPort, "probe:")
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+probePort, 2*time.Second); err == nil {
			c.Close()
			conn.Write([]byte("OK\n"))
		} else {
			conn.Write([]byte("ERR:X-UI Port Refused\n"))
		}
		return
	}

	target, err := net.DialTimeout("tcp", "127.0.0.1:"+targetPort, 5*time.Second)
	if err != nil {
		addLog(fmt.Sprintf("X-UI ERR [Forward %s]: Local 127.0.0.1:%s refused", t.Name, targetPort))
		return
	}
	defer target.Close()
	enableKeepAlive(target)

	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		br.Read(buf)
		target.Write(buf)
	}

	mbLimit, _ := strconv.Atoi(t.SpeedLimit)
	txW := newThrottledWriter(target, &appRxBytes, mbLimit)
	rxW := newThrottledWriter(conn, &appTxBytes, mbLimit)

	done := make(chan struct{}, 2)
	go func() { io.Copy(txW, conn); done <- struct{}{} }()
	go func() { io.Copy(rxW, target); done <- struct{}{} }()
	<-done
}
