package autoplay

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Manager coordinates CS2 bots across all running sandboxes.
type Manager struct {
	mu   sync.Mutex
	bots map[int64]*CS2Bot // accountID -> bot
	gsi  *GSIServer
	ctx  context.Context
}

func NewManager(ctx context.Context) *Manager {
	gsi := NewGSIServer()
	gsi.Start()

	if err := EnsureGSIConfig(); err != nil {
		log.Printf("[Autoplay] GSI config warning: %v (CS2 GSI may not work)", err)
	}

	return &Manager{
		bots: make(map[int64]*CS2Bot),
		gsi:  gsi,
		ctx:  ctx,
	}
}

// StartBot creates and launches a CS2 bot for the given sandbox.
// display is the Xvfb display number (e.g. 100).
func (m *Manager) StartBot(accountID int64, display int, steamID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[accountID]; exists {
		return fmt.Errorf("bot for account %d already running", accountID)
	}

	bot, err := NewCS2Bot(BotConfig{
		Display: display,
		SteamID: steamID,
		GSI:     m.gsi,
	})
	if err != nil {
		return fmt.Errorf("create bot: %w", err)
	}

	bot.Start(m.ctx)
	m.bots[accountID] = bot
	log.Printf("[Autoplay] Bot started for account %d on display :%d", accountID, display)
	return nil
}

func (m *Manager) StopBot(accountID int64) {
	m.mu.Lock()
	bot, ok := m.bots[accountID]
	if ok {
		delete(m.bots, accountID)
	}
	m.mu.Unlock()

	if ok {
		bot.Stop()
		log.Printf("[Autoplay] Bot stopped for account %d", accountID)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.bots))
	for id := range m.bots {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.StopBot(id)
	}
}

func (m *Manager) GetStatus(accountID int64) *BotStatus {
	m.mu.Lock()
	bot, ok := m.bots[accountID]
	m.mu.Unlock()

	if !ok {
		return nil
	}
	s := bot.Status()
	return &s
}

func (m *Manager) AllStatuses() []BotStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]BotStatus, 0, len(m.bots))
	for _, bot := range m.bots {
		result = append(result, bot.Status())
	}
	return result
}

func (m *Manager) Shutdown() {
	m.StopAll()
	m.gsi.Stop()
}
