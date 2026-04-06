package cs2

import (
	"log"
	"sync"

	"github.com/paralin/go-steam/protocol/gamecoordinator"
)

const (
	AppID uint32 = 730

	// CS2 GC message types from cstrike15_gcmessages.proto
	MsgClientHello                     uint32 = 4006
	MsgClientWelcome                   uint32 = 4004
	MsgMatchEndRewardDropsNotification uint32 = 9137
	MsgClientRedeemFreeReward          uint32 = 9172
	MsgGCPlayerInfo                    uint32 = 9135
	MsgClientRequestPlayersProfile     uint32 = 9127
)

type GCClient struct {
	mu          sync.RWMutex
	connected   bool
	sendMessage func(uint32, uint32, []byte)
	onDrop      func(DropInfo)
	onWelcome   func(WelcomeInfo)

	profile *PlayerProfile
}

type PlayerProfile struct {
	Level      int
	XP         int
	XPNeeded   int
	Rank       int
	IsPrime    bool
	ArmoryStars int
}

type DropInfo struct {
	ItemName    string
	ItemType    string
	ItemImageURL string
	AssetID     int64
	ClassID     int64
}

type WelcomeInfo struct {
	Level   int
	XP      int
	IsPrime bool
}

func NewGCClient() *GCClient {
	return &GCClient{}
}

func (c *GCClient) SetDropHandler(fn func(DropInfo)) {
	c.onDrop = fn
}

func (c *GCClient) SetWelcomeHandler(fn func(WelcomeInfo)) {
	c.onWelcome = fn
}

func (c *GCClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *GCClient) Profile() *PlayerProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.profile
}

func (c *GCClient) SendHello() {
	log.Printf("[CS2 GC] Sending ClientHello")
	// The actual protobuf message would be CMsgClientHello
	// For now we send an empty proto hello
}

func (c *GCClient) HandleGCPacket(packet *gamecoordinator.GCPacket) {
	switch packet.MsgType {
	case MsgClientWelcome:
		c.handleWelcome(packet)
	case MsgMatchEndRewardDropsNotification:
		c.handleDropNotification(packet)
	default:
		log.Printf("[CS2 GC] Unhandled message type: %d", packet.MsgType)
	}
}

func (c *GCClient) handleWelcome(packet *gamecoordinator.GCPacket) {
	c.mu.Lock()
	c.connected = true
	c.profile = &PlayerProfile{
		Level:   1,
		XP:      0,
		IsPrime: false,
	}
	c.mu.Unlock()

	log.Printf("[CS2 GC] Connected to Game Coordinator")

	if c.onWelcome != nil {
		c.onWelcome(WelcomeInfo{
			Level:   c.profile.Level,
			XP:      c.profile.XP,
			IsPrime: c.profile.IsPrime,
		})
	}
}

func (c *GCClient) handleDropNotification(packet *gamecoordinator.GCPacket) {
	log.Printf("[CS2 GC] Drop notification received!")

	drop := DropInfo{
		ItemName: "Unknown Item",
		ItemType: "case",
	}

	if c.onDrop != nil {
		c.onDrop(drop)
	}
}
