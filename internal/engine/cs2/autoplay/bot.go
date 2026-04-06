package autoplay

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"
)

type BotPhase int

const (
	PhaseLoading    BotPhase = iota // waiting for game to load
	PhaseAlive                      // in-game, alive
	PhaseDead                       // in-game, dead (waiting for respawn)
	PhaseFreezeTime                 // round freezetime
)

func (p BotPhase) String() string {
	switch p {
	case PhaseLoading:
		return "loading"
	case PhaseAlive:
		return "alive"
	case PhaseDead:
		return "dead"
	case PhaseFreezeTime:
		return "freezetime"
	default:
		return "unknown"
	}
}

type CS2Bot struct {
	display int
	steamID string
	input   *InputSender
	gsi     *GSIServer

	mu         sync.Mutex
	phase      BotPhase
	lastGSI    *GSIState
	cancel     context.CancelFunc
	shooting   bool
	heldKeys   map[uint]bool
	running    bool
	startedAt  time.Time
	kills      int
	deaths     int
}

type BotConfig struct {
	Display int
	SteamID string
	GSI     *GSIServer
}

type BotStatus struct {
	Display int    `json:"display"`
	SteamID string `json:"steam_id"`
	Phase   string `json:"phase"`
	Running bool   `json:"running"`
	Uptime  string `json:"uptime"`
	Kills   int    `json:"kills"`
	Deaths  int    `json:"deaths"`
	Health  int    `json:"health"`
	Map     string `json:"map"`
}

func NewCS2Bot(cfg BotConfig) (*CS2Bot, error) {
	input, err := NewInputSender(cfg.Display)
	if err != nil {
		return nil, err
	}

	return &CS2Bot{
		display:  cfg.Display,
		steamID:  cfg.SteamID,
		input:    input,
		gsi:      cfg.GSI,
		phase:    PhaseLoading,
		heldKeys: make(map[uint]bool),
	}, nil
}

func (b *CS2Bot) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	if b.gsi != nil && b.steamID != "" {
		b.gsi.RegisterHandler(b.steamID, b.onGSIUpdate)
	}

	b.mu.Lock()
	b.running = true
	b.startedAt = time.Now()
	b.mu.Unlock()

	go b.run(ctx)
	log.Printf("[CS2Bot] Started for display :%d (steamID=%s)", b.display, b.steamID)
}

func (b *CS2Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.gsi != nil && b.steamID != "" {
		b.gsi.UnregisterHandler(b.steamID)
	}
	b.releaseAll()
	b.input.Close()

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	log.Printf("[CS2Bot] Stopped for display :%d", b.display)
}

func (b *CS2Bot) Status() BotStatus {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := BotStatus{
		Display: b.display,
		SteamID: b.steamID,
		Phase:   b.phase.String(),
		Running: b.running,
		Kills:   b.kills,
		Deaths:  b.deaths,
	}
	if b.running {
		s.Uptime = time.Since(b.startedAt).Truncate(time.Second).String()
	}
	if b.lastGSI != nil {
		if b.lastGSI.Player != nil && b.lastGSI.Player.State != nil {
			s.Health = b.lastGSI.Player.State.Health
		}
		if b.lastGSI.Map != nil {
			s.Map = b.lastGSI.Map.Name
		}
	}
	return s
}

func (b *CS2Bot) onGSIUpdate(state *GSIState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	prev := b.lastGSI
	b.lastGSI = state

	if state.Player == nil || state.Player.State == nil {
		return
	}

	wasAlive := prev != nil && prev.Player != nil && prev.Player.State != nil && prev.Player.State.Health > 0
	nowAlive := state.Player.State.Health > 0

	if wasAlive && !nowAlive {
		b.deaths++
		b.phase = PhaseDead
	} else if nowAlive {
		if state.Round != nil && state.Round.Phase == "freezetime" {
			b.phase = PhaseFreezeTime
		} else {
			b.phase = PhaseAlive
		}
	} else {
		b.phase = PhaseDead
	}
}

func (b *CS2Bot) run(ctx context.Context) {
	// Phase 1: wait for CS2 to load (~45s for local DM)
	log.Printf("[CS2Bot:%d] Waiting for game to load...", b.display)
	select {
	case <-ctx.Done():
		return
	case <-time.After(50 * time.Second):
	}

	// Dismiss any startup dialogs
	b.dismissDialogs(ctx)

	// If GSI hasn't reported yet, assume alive (local DM auto-spawns)
	b.mu.Lock()
	if b.lastGSI == nil {
		b.phase = PhaseAlive
		log.Printf("[CS2Bot:%d] No GSI data — assuming alive (local DM)", b.display)
	} else {
		log.Printf("[CS2Bot:%d] GSI active, phase=%s", b.display, b.phase.String())
	}
	b.mu.Unlock()

	// Phase 2: main bot loop
	moveTick := time.NewTicker(50 * time.Millisecond)  // 20 Hz: micro mouse adjustments
	planTick := time.NewTicker(2 * time.Second)         // new movement plan
	combatTick := time.NewTicker(150 * time.Millisecond) // combat decisions
	defer moveTick.Stop()
	defer planTick.Stop()
	defer combatTick.Stop()

	b.newMovePlan()

	for {
		select {
		case <-ctx.Done():
			b.releaseAll()
			return
		case <-moveTick.C:
			b.tickMove()
		case <-planTick.C:
			b.tickPlan()
			planTick.Reset(time.Duration(1500+rand.Intn(3500)) * time.Millisecond)
		case <-combatTick.C:
			b.tickCombat()
		}
	}
}

// ---------- movement ----------

var movementPatterns = []struct {
	keys   []uint
	weight int
}{
	{[]uint{KeyW}, 35},                // forward
	{[]uint{KeyW, KeyA}, 15},          // forward + left
	{[]uint{KeyW, KeyD}, 15},          // forward + right
	{[]uint{KeyA}, 5},                 // strafe left
	{[]uint{KeyD}, 5},                 // strafe right
	{[]uint{KeyS}, 3},                 // backward
	{[]uint{KeyW, KeyShiftL}, 7},      // walk forward (quiet)
	{[]uint{}, 5},                     // stand still (aim)
	{[]uint{KeyW, KeyCtrlL}, 5},       // crouch walk
	{[]uint{KeyS, KeyA}, 3},           // back-left
	{[]uint{KeyS, KeyD}, 2},           // back-right
}

func pickPattern() []uint {
	total := 0
	for _, p := range movementPatterns {
		total += p.weight
	}
	r := rand.Intn(total)
	for _, p := range movementPatterns {
		r -= p.weight
		if r < 0 {
			return p.keys
		}
	}
	return movementPatterns[0].keys
}

func (b *CS2Bot) newMovePlan() {
	b.releaseMovement()

	keys := pickPattern()
	for _, k := range keys {
		b.holdKey(k)
	}

	// Random turn while changing direction
	dx := rand.Intn(81) - 40 // -40 .. 40
	dy := rand.Intn(21) - 10 // -10 .. 10
	b.input.MouseMove(dx, dy)
}

func (b *CS2Bot) tickMove() {
	b.mu.Lock()
	phase := b.phase
	b.mu.Unlock()

	if phase != PhaseAlive {
		return
	}

	// Small random mouse drift for natural look
	if rand.Intn(4) == 0 {
		dx := rand.Intn(5) - 2
		dy := rand.Intn(3) - 1
		b.input.MouseMove(dx, dy)
	}
}

func (b *CS2Bot) tickPlan() {
	b.mu.Lock()
	phase := b.phase
	b.mu.Unlock()

	switch phase {
	case PhaseAlive:
		b.newMovePlan()

		// Occasional jump
		if rand.Intn(6) == 0 {
			b.input.KeyTap(KeySpace)
		}
		// Occasional weapon switch
		if rand.Intn(12) == 0 {
			wk := []uint{Key1, Key2, Key3}[rand.Intn(3)]
			b.input.KeyTap(wk)
		}
		// Occasional reload
		if rand.Intn(10) == 0 {
			b.input.KeyTap(KeyR)
		}

	case PhaseDead, PhaseFreezeTime:
		b.releaseAll()

	case PhaseLoading:
		// still waiting
	}
}

func (b *CS2Bot) tickCombat() {
	b.mu.Lock()
	phase := b.phase
	b.mu.Unlock()

	if phase != PhaseAlive {
		if b.shooting {
			b.input.MouseUp(1)
			b.shooting = false
		}
		return
	}

	r := rand.Intn(100)
	switch {
	case r < 25:
		// Start/continue burst
		if !b.shooting {
			b.input.MouseDown(1)
			b.shooting = true
		}
	case r < 40:
		// Single tap
		if b.shooting {
			b.input.MouseUp(1)
			b.shooting = false
		}
		b.input.Click(1)
	case r < 60:
		// Stop shooting
		if b.shooting {
			b.input.MouseUp(1)
			b.shooting = false
		}
	default:
		// No change — keep current state
	}

	// Bigger sweeps to simulate target tracking
	if rand.Intn(8) == 0 {
		dx := rand.Intn(51) - 25
		dy := rand.Intn(15) - 7
		b.input.MouseMove(dx, dy)
	}
}

// ---------- input helpers ----------

func (b *CS2Bot) holdKey(keysym uint) {
	b.mu.Lock()
	if !b.heldKeys[keysym] {
		b.heldKeys[keysym] = true
		b.mu.Unlock()
		b.input.KeyDown(keysym)
	} else {
		b.mu.Unlock()
	}
}

func (b *CS2Bot) releaseKey(keysym uint) {
	b.mu.Lock()
	if b.heldKeys[keysym] {
		delete(b.heldKeys, keysym)
		b.mu.Unlock()
		b.input.KeyUp(keysym)
	} else {
		b.mu.Unlock()
	}
}

var allMoveKeys = []uint{KeyW, KeyA, KeyS, KeyD, KeyShiftL, KeyCtrlL, KeySpace}

func (b *CS2Bot) releaseMovement() {
	for _, k := range allMoveKeys {
		b.releaseKey(k)
	}
}

func (b *CS2Bot) releaseAll() {
	b.releaseMovement()
	if b.shooting {
		b.input.MouseUp(1)
		b.shooting = false
	}
}

func (b *CS2Bot) dismissDialogs(ctx context.Context) {
	// Click center to dismiss overlays
	b.input.Click(1)
	sleepCtx(ctx, 500*time.Millisecond)

	// Escape to close menus
	b.input.KeyTap(KeyEscape)
	sleepCtx(ctx, 400*time.Millisecond)
	b.input.KeyTap(KeyEscape)
	sleepCtx(ctx, 400*time.Millisecond)

	// Another click
	b.input.Click(1)
	sleepCtx(ctx, 300*time.Millisecond)
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
