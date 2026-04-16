package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type VNCProxy struct {
	upgrader websocket.Upgrader
}

func NewVNCProxy() *VNCProxy {
	return &VNCProxy{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

// Handle proxies WebSocket from browser to VNC server on a container.
// URL: /vnc/{port} — connects to localhost:{port} (VNC port of container)
func (p *VNCProxy) Handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/vnc/"), "/")
	if len(parts) == 0 {
		http.Error(w, "missing port", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(parts[0])
	// Не ограничивать 5900–6100: бывают 5800+N, кастомные порты Docker и т.д.
	if err != nil || port < 1024 || port > 65535 {
		http.Error(w, "invalid VNC port", http.StatusBadRequest)
		return
	}

	vncConn, vncAddrUsed, err := dialVNCLocalhost(port)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to VNC on port %d (tried 127.0.0.1 and ::1): %v", port, err), http.StatusBadGateway)
		return
	}

	wsConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		vncConn.Close()
		log.Printf("[VNC Proxy] WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("[VNC Proxy] Bridging WebSocket <-> VNC %s (port %d)", vncAddrUsed, port)

	go func() {
		defer vncConn.Close()
		defer wsConn.Close()
		for {
			_, msg, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			if _, err := vncConn.Write(msg); err != nil {
				return
			}
		}
	}()

	go func() {
		defer vncConn.Close()
		defer wsConn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := vncConn.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("[VNC Proxy] VNC read error: %v", err)
				}
				return
			}
			if n == 0 {
				continue
			}
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()
}

// dialVNCLocalhost: x11vnc иногда слушает только IPv6 [::], тогда 127.0.0.1 не подключается.
func dialVNCLocalhost(port int) (net.Conn, string, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	addrs := []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("[::1]:%d", port),
	}
	var lastErr error
	for _, a := range addrs {
		c, err := d.Dial("tcp", a)
		if err == nil {
			return c, a, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}
