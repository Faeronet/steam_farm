package ws

import (
	"encoding/json"
	"log"
	"sync"
)

type EventType string

const (
	EventFarmStatus    EventType = "farm:status"
	EventDropReceived  EventType = "farm:drop"
	EventRewardPending EventType = "farm:reward"
	EventSandboxChange EventType = "sandbox:status"
	EventError         EventType = "farm:error"
	EventLog           EventType = "log"
	EventYoloLog       EventType = "yolo:log"
	EventCS2Mem        EventType = "cs2:mem"
	EventSigScanLog    EventType = "sigscan:log"
)

type WSMessage struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool

	broadcast  chan WSMessage
	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan WSMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WS Hub] Client connected (%d total)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[WS Hub] Client disconnected (%d total)", len(h.clients))

		case message := <-h.broadcast:
			data, err := json.Marshal(message)
			if err != nil {
				continue
			}
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(eventType EventType, payload interface{}) {
	h.broadcast <- WSMessage{
		Type:    eventType,
		Payload: payload,
	}
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
