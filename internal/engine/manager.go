package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/faeronet/steam-farm-system/internal/database/models"
	steamclient "github.com/faeronet/steam-farm-system/internal/engine/steam"
)

type Manager struct {
	mu   sync.RWMutex
	bots map[int64]*Bot

	onStatusChange func(accountID int64, status models.AccountStatus, detail string)
	onDrop         func(accountID int64, drop models.Drop)
	onReward       func(accountID int64, choices interface{})
}

func NewManager() *Manager {
	return &Manager{
		bots: make(map[int64]*Bot),
	}
}

func (m *Manager) SetStatusHandler(fn func(int64, models.AccountStatus, string)) {
	m.onStatusChange = fn
}

func (m *Manager) SetDropHandler(fn func(int64, models.Drop)) {
	m.onDrop = fn
}

func (m *Manager) SetRewardHandler(fn func(int64, interface{})) {
	m.onReward = fn
}

func (m *Manager) StartBot(ctx context.Context, account models.Account, password string, gameType ...models.GameType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[account.ID]; exists {
		return fmt.Errorf("bot for account %d already running", account.ID)
	}

	var gt models.GameType
	if len(gameType) > 0 && gameType[0] != "" {
		gt = gameType[0]
	}

	bot := NewBot(account, password, gt, m)
	m.bots[account.ID] = bot

	go func() {
		if err := bot.Start(ctx); err != nil {
			log.Printf("[Manager] Bot %s failed: %v", account.Username, err)
			m.notifyStatus(account.ID, models.StatusError, err.Error())
		}
	}()

	m.notifyStatus(account.ID, models.StatusQueued, "starting")
	return nil
}

func (m *Manager) StopBot(accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	bot, exists := m.bots[accountID]
	if !exists {
		return fmt.Errorf("bot for account %d not found", accountID)
	}

	bot.Stop()
	delete(m.bots, accountID)
	m.notifyStatus(accountID, models.StatusIdle, "stopped")
	return nil
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, bot := range m.bots {
		bot.Stop()
		delete(m.bots, id)
	}
}

func (m *Manager) GetBotStatus(accountID int64) (steamclient.ClientState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bot, exists := m.bots[accountID]
	if !exists {
		return steamclient.StateDisconnected, false
	}

	return bot.State(), true
}

func (m *Manager) ActiveBots() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.bots)
}

// GetPlaytimes returns a map of accountID -> session hours for all active bots.
func (m *Manager) GetPlaytimes() map[int64]float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[int64]float32, len(m.bots))
	for id, bot := range m.bots {
		result[id] = bot.PlaytimeHours()
	}
	return result
}

func (m *Manager) notifyStatus(accountID int64, status models.AccountStatus, detail string) {
	if m.onStatusChange != nil {
		m.onStatusChange(accountID, status, detail)
	}
}
