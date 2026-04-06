package dota2

import (
	"sync"
	"time"
)

type FarmState struct {
	mu sync.RWMutex

	AccountID   int64
	StartedAt   time.Time
	TotalHours  float64
	EventName   string
	EventLevel  int
	EventAct    int
	EventNode   int
	TokensEarned int
}

func NewFarmState(accountID int64) *FarmState {
	return &FarmState{
		AccountID: accountID,
		StartedAt: time.Now(),
	}
}

func (f *FarmState) SessionHours() float64 {
	return time.Since(f.StartedAt).Hours()
}

func (f *FarmState) UpdateEvent(name string, level, act, node, tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.EventName = name
	f.EventLevel = level
	f.EventAct = act
	f.EventNode = node
	f.TokensEarned = tokens
}

func (f *FarmState) Snapshot() FarmSnapshot {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return FarmSnapshot{
		AccountID:    f.AccountID,
		HoursPlayed:  time.Since(f.StartedAt).Hours(),
		TotalHours:   f.TotalHours + time.Since(f.StartedAt).Hours(),
		EventName:    f.EventName,
		EventLevel:   f.EventLevel,
		EventAct:     f.EventAct,
		EventNode:    f.EventNode,
		TokensEarned: f.TokensEarned,
	}
}

type FarmSnapshot struct {
	AccountID    int64   `json:"account_id"`
	HoursPlayed  float64 `json:"hours_played"`
	TotalHours   float64 `json:"total_hours"`
	EventName    string  `json:"event_name"`
	EventLevel   int     `json:"event_level"`
	EventAct     int     `json:"event_act"`
	EventNode    int     `json:"event_node"`
	TokensEarned int     `json:"tokens_earned"`
}
