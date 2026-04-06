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
	if err != nil || port < 5900 || port > 6100 {
		http.Error(w, "invalid VNC port", http.StatusBadRequest)
		return
	}

	vncAddr := fmt.Sprintf("127.0.0.1:%d", port)
	vncConn, err := net.DialTimeout("tcp", vncAddr, 3*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to VNC at %s: %v", vncAddr, err), http.StatusBadGateway)
		return
	}

	wsConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		vncConn.Close()
		log.Printf("[VNC Proxy] WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("[VNC Proxy] Bridging WebSocket <-> VNC port %d", port)

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
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()
}
