package cs2

import (
	"log"
	"sync"
	"time"
)

type FarmState struct {
	mu sync.RWMutex

	AccountID    int64
	StartedAt    time.Time
	HoursPlayed  float64
	XPAtStart    int
	XPCurrent    int
	Level        int
	IsPrime      bool
	DropsThisWeek int
	FarmedThisWeek bool
	DropAvailable  bool

	ArmoryStars int
}

func NewFarmState(accountID int64) *FarmState {
	return &FarmState{
		AccountID: accountID,
		StartedAt: time.Now(),
	}
}

func (f *FarmState) UpdateFromProfile(profile *PlayerProfile) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if profile == nil {
		return
	}

	f.Level = profile.Level
	f.XPCurrent = profile.XP
	f.IsPrime = profile.IsPrime
	f.ArmoryStars = profile.ArmoryStars
}

func (f *FarmState) RecordDrop() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.DropsThisWeek++
	f.FarmedThisWeek = true
	f.DropAvailable = true

	log.Printf("[CS2 Farm] Account %d received drop #%d this week", f.AccountID, f.DropsThisWeek)
}

func (f *FarmState) MarkCollected() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DropAvailable = false
}

func (f *FarmState) SessionHours() float64 {
	return time.Since(f.StartedAt).Hours()
}

func (f *FarmState) Snapshot() FarmSnapshot {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return FarmSnapshot{
		AccountID:      f.AccountID,
		HoursPlayed:    time.Since(f.StartedAt).Hours(),
		Level:          f.Level,
		XP:             f.XPCurrent,
		IsPrime:        f.IsPrime,
		DropsThisWeek:  f.DropsThisWeek,
		FarmedThisWeek: f.FarmedThisWeek,
		DropAvailable:  f.DropAvailable,
		ArmoryStars:    f.ArmoryStars,
	}
}

type FarmSnapshot struct {
	AccountID      int64   `json:"account_id"`
	HoursPlayed    float64 `json:"hours_played"`
	Level          int     `json:"level"`
	XP             int     `json:"xp"`
	IsPrime        bool    `json:"is_prime"`
	DropsThisWeek  int     `json:"drops_this_week"`
	FarmedThisWeek bool    `json:"farmed_this_week"`
	DropAvailable  bool    `json:"drop_available"`
	ArmoryStars    int     `json:"armory_stars"`
}
