package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/faeronet/steam-farm-system/internal/database/models"
	steamclient "github.com/faeronet/steam-farm-system/internal/engine/steam"
)

const (
	AppIDCS2   uint64 = 730
	AppIDDota2 uint64 = 570
)

type Bot struct {
	account  models.Account
	password string
	gameType models.GameType
	client   *steamclient.Client
	manager  *Manager
	cancel   context.CancelFunc

	mu           sync.Mutex
	playingSince *time.Time
}

func NewBot(account models.Account, password string, gameType models.GameType, manager *Manager) *Bot {
	gt := gameType
	if gt == "" {
		gt = account.GameType
	}
	return &Bot{
		account:  account,
		password: password,
		gameType: gt,
		manager:  manager,
	}
}

func (b *Bot) Start(ctx context.Context) error {
	childCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	var sharedSecret string
	if b.account.SharedSecret != nil {
		sharedSecret = *b.account.SharedSecret
	}

	b.client = steamclient.NewClient(steamclient.ClientConfig{
		Username:     b.account.Username,
		Password:     b.password,
		SharedSecret: sharedSecret,
		AppID:        b.appID(),
		OnEvent:      b.handleEvent,
	})

	log.Printf("[Bot %s] Starting (game=%s, appID=%d)...", b.account.Username, b.gameType, b.appID())

	return b.client.Run(childCtx)
}

func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *Bot) State() steamclient.ClientState {
	if b.client == nil {
		return steamclient.StateDisconnected
	}
	return b.client.State()
}

func (b *Bot) GameType() models.GameType {
	return b.gameType
}

func (b *Bot) appID() uint64 {
	if b.gameType == models.GameDota2 {
		return AppIDDota2
	}
	return AppIDCS2
}

func (b *Bot) gameName() string {
	if b.gameType == models.GameDota2 {
		return "Dota 2"
	}
	return "CS2"
}

// PlaytimeHours returns hours played in the current session.
func (b *Bot) PlaytimeHours() float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.playingSince == nil {
		return 0
	}
	return float32(time.Since(*b.playingSince).Hours())
}

func (b *Bot) handleEvent(event interface{}) {
	switch e := event.(type) {
	case *steamclient.LoggedInEvent:
		log.Printf("[Bot %s] Logged in (SteamID=%d), farming %s",
			b.account.Username, e.SteamID, b.gameName())
		b.manager.notifyStatus(b.account.ID, models.StatusFarming, "playing "+b.gameName())
		b.mu.Lock()
		now := time.Now()
		b.playingSince = &now
		b.mu.Unlock()

	case *steamclient.ReconnectedEvent:
		log.Printf("[Bot %s] Reconnected (SteamID=%d), resumed %s",
			b.account.Username, e.SteamID, b.gameName())
		b.manager.notifyStatus(b.account.ID, models.StatusFarming, "reconnected, playing "+b.gameName())
		b.mu.Lock()
		now := time.Now()
		b.playingSince = &now
		b.mu.Unlock()

	case *steamclient.ConnectionLostEvent:
		log.Printf("[Bot %s] Connection lost: %s — reconnecting...", b.account.Username, e.Reason)
		b.manager.notifyStatus(b.account.ID, models.StatusQueued, "reconnecting...")
		b.mu.Lock()
		b.playingSince = nil
		b.mu.Unlock()

	case *steamclient.LoginFailedEvent:
		log.Printf("[Bot %s] Login failed: %s", b.account.Username, e.Reason)
		b.manager.notifyStatus(b.account.ID, models.StatusError, "login failed: "+e.Reason)
		b.mu.Lock()
		b.playingSince = nil
		b.mu.Unlock()
	}
}
