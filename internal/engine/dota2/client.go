package dota2

import (
	"log"
	"sync"

	"github.com/paralin/go-steam/protocol/gamecoordinator"
)

const (
	AppID uint32 = 570

	MsgClientHello   uint32 = 7408
	MsgClientWelcome uint32 = 7005
	MsgSOUpdate      uint32 = 7046
)

type GCClient struct {
	mu        sync.RWMutex
	connected bool
	onEvent   func(EventInfo)

	profile *DotaProfile
}

type DotaProfile struct {
	AccountID   uint32
	Hours       float64
	Level       int
	EventActive bool
	EventName   string
}

type EventInfo struct {
	Type    string
	Payload interface{}
}

func NewGCClient() *GCClient {
	return &GCClient{}
}

func (c *GCClient) SetEventHandler(fn func(EventInfo)) {
	c.onEvent = fn
}

func (c *GCClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *GCClient) Profile() *DotaProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.profile
}

func (c *GCClient) SendHello() {
	log.Printf("[Dota2 GC] Sending ClientHello")
}

func (c *GCClient) HandleGCPacket(packet *gamecoordinator.GCPacket) {
	switch packet.MsgType {
	case MsgClientWelcome:
		c.handleWelcome(packet)
	case MsgSOUpdate:
		c.handleSOUpdate(packet)
	default:
		log.Printf("[Dota2 GC] Unhandled message type: %d", packet.MsgType)
	}
}

func (c *GCClient) handleWelcome(packet *gamecoordinator.GCPacket) {
	c.mu.Lock()
	c.connected = true
	c.profile = &DotaProfile{
		Hours: 0,
		Level: 0,
	}
	c.mu.Unlock()

	log.Printf("[Dota2 GC] Connected to Game Coordinator")

	if c.onEvent != nil {
		c.onEvent(EventInfo{Type: "welcome"})
	}
}

func (c *GCClient) handleSOUpdate(packet *gamecoordinator.GCPacket) {
	log.Printf("[Dota2 GC] SOCache update received")
	// SOCache updates contain lobby info, event progress, etc.
}
