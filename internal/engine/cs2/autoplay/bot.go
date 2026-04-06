package autoplay

import (
	"context"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"
)

type BotPhase int

const (
	PhaseLoading    BotPhase = iota
	PhaseAlive
	PhaseDead
	PhaseFreezeTime
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

// Behavior types the bot cycles through
type behaviorKind int

const (
	bhvPatrol      behaviorKind = iota // walk forward, smooth turns
	bhvCornerCheck                     // pause, look left then right
	bhvSprint                          // fast run + occasional jump
	bhvCombat                          // stop/crouch, shoot in bursts, aim-track
	bhvReposition                      // backpedal or strafe, then resume
)

// Smooth turn state — the bot maintains a "desired turn rate" that changes
// gradually, producing curved mouse paths instead of random jumps.
type turnState struct {
	yawRate   float64 // degrees per second (negative = left)
	pitchRate float64 // degrees per second (negative = up)
	yawAccum  float64 // sub-pixel accumulator
	pitchAccm float64
}

type CS2Bot struct {
	display int
	steamID string
	input   *InputSender
	gsi     *GSIServer

	mu        sync.Mutex
	phase     BotPhase
	lastGSI   *GSIState
	cancel    context.CancelFunc
	shooting  bool
	heldKeys  map[uint]bool
	running   bool
	startedAt time.Time
	kills     int
	deaths    int

	// Current behavior
	behavior    behaviorKind
	bhvStart    time.Time
	bhvDuration time.Duration
	turn        turnState

	// Combat sub-state
	burstTicks int // remaining ticks in current burst
	burstCool  int // cooldown ticks before next burst
}

type BotConfig struct {
	Display int
	SteamID string
	GSI     *GSIServer
}

type BotStatus struct {
	Display  int    `json:"display"`
	SteamID  string `json:"steam_id"`
	Phase    string `json:"phase"`
	Running  bool   `json:"running"`
	Uptime   string `json:"uptime"`
	Kills    int    `json:"kills"`
	Deaths   int    `json:"deaths"`
	Health   int    `json:"health"`
	Map      string `json:"map"`
	Behavior string `json:"behavior"`
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
		Display:  b.display,
		SteamID:  b.steamID,
		Phase:    b.phase.String(),
		Running:  b.running,
		Kills:    b.kills,
		Deaths:   b.deaths,
		Behavior: bhvName(b.behavior),
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

func bhvName(k behaviorKind) string {
	switch k {
	case bhvPatrol:
		return "patrol"
	case bhvCornerCheck:
		return "corner-check"
	case bhvSprint:
		return "sprint"
	case bhvCombat:
		return "combat"
	case bhvReposition:
		return "reposition"
	default:
		return "idle"
	}
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

// ─────────────────────── main loop ───────────────────────

const tickRate = 20 * time.Millisecond // 50 Hz — smooth enough for natural movement

func (b *CS2Bot) run(ctx context.Context) {
	log.Printf("[CS2Bot:%d] Waiting 55s for game to load...", b.display)
	if !sleepCtx(ctx, 55*time.Second) {
		return
	}

	b.ensureFocus(ctx)

	b.mu.Lock()
	if b.lastGSI == nil {
		b.phase = PhaseAlive
		log.Printf("[CS2Bot:%d] No GSI — assuming alive (local DM)", b.display)
	} else {
		log.Printf("[CS2Bot:%d] GSI active, phase=%s", b.display, b.phase.String())
	}
	b.mu.Unlock()

	b.pickBehavior()

	ticker := time.NewTicker(tickRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.releaseAll()
			return
		case <-ticker.C:
			b.tick(ctx)
		}
	}
}

func (b *CS2Bot) tick(ctx context.Context) {
	b.mu.Lock()
	phase := b.phase
	b.mu.Unlock()

	switch phase {
	case PhaseAlive:
		if time.Since(b.bhvStart) > b.bhvDuration {
			b.pickBehavior()
		}
		b.executeBehavior()

	case PhaseDead:
		b.releaseAll()
		// In DM auto-respawn is quick; just wait
	case PhaseFreezeTime:
		b.releaseAll()
	case PhaseLoading:
		// still waiting
	}
}

// ─────────────────────── behavior selection ───────────────────────

// Weighted random behavior picker that mimics a hard training bot's cycle:
// mostly patrolling, with periodic combat encounters and repositioning.
func (b *CS2Bot) pickBehavior() {
	b.releaseAll()

	weights := []struct {
		kind   behaviorKind
		weight int
	}{
		{bhvPatrol, 40},
		{bhvSprint, 15},
		{bhvCornerCheck, 12},
		{bhvCombat, 25},
		{bhvReposition, 8},
	}

	total := 0
	for _, w := range weights {
		total += w.weight
	}
	r := rand.Intn(total)
	chosen := bhvPatrol
	for _, w := range weights {
		r -= w.weight
		if r < 0 {
			chosen = w.kind
			break
		}
	}

	b.behavior = chosen
	b.bhvStart = time.Now()

	switch chosen {
	case bhvPatrol:
		b.bhvDuration = randDur(4000, 10000)
		b.initPatrol()
	case bhvSprint:
		b.bhvDuration = randDur(2000, 5000)
		b.initSprint()
	case bhvCornerCheck:
		b.bhvDuration = randDur(2500, 4500)
		b.initCornerCheck()
	case bhvCombat:
		b.bhvDuration = randDur(3000, 7000)
		b.initCombat()
	case bhvReposition:
		b.bhvDuration = randDur(1500, 3500)
		b.initReposition()
	}
}

func (b *CS2Bot) executeBehavior() {
	elapsed := time.Since(b.bhvStart)
	switch b.behavior {
	case bhvPatrol:
		b.tickPatrol(elapsed)
	case bhvSprint:
		b.tickSprint(elapsed)
	case bhvCornerCheck:
		b.tickCornerCheck(elapsed)
	case bhvCombat:
		b.tickCombat(elapsed)
	case bhvReposition:
		b.tickReposition(elapsed)
	}
}

// ─────────────────────── PATROL ───────────────────────
// Walk forward continuously with a smooth sinusoidal yaw drift.
// This traces gentle S-curves through the map — very natural.

func (b *CS2Bot) initPatrol() {
	b.holdKey(KeyW) // forward the entire time

	// Gentle turn: ±15-45 deg/s, with slight pitch oscillation
	b.turn.yawRate = (15 + rand.Float64()*30) * randSign()
	b.turn.pitchRate = (2 + rand.Float64()*4) * randSign()
	b.turn.yawAccum = 0
	b.turn.pitchAccm = 0
}

func (b *CS2Bot) tickPatrol(elapsed time.Duration) {
	b.ensureHeld(KeyW)

	dt := tickRate.Seconds()
	phase := elapsed.Seconds()

	// Smooth sinusoidal yaw oscillation: the turn rate itself drifts slowly
	yaw := b.turn.yawRate * (1.0 + 0.3*math.Sin(phase*0.7))
	pitch := b.turn.pitchRate * math.Sin(phase*1.2)

	b.smoothMouse(yaw*dt, pitch*dt)

	// Occasional subtle mouse jitter (hand tremor)
	if rand.Intn(8) == 0 {
		b.input.MouseMove(rand.Intn(3)-1, rand.Intn(3)-1)
	}
}

// ─────────────────────── SPRINT ───────────────────────
// Run forward fast with slight turns, occasional jump

func (b *CS2Bot) initSprint() {
	b.holdKey(KeyW)
	b.turn.yawRate = (10 + rand.Float64()*20) * randSign()
	b.turn.pitchRate = 0
	b.turn.yawAccum = 0
	b.turn.pitchAccm = 0
}

func (b *CS2Bot) tickSprint(elapsed time.Duration) {
	b.ensureHeld(KeyW)

	dt := tickRate.Seconds()
	yaw := b.turn.yawRate
	b.smoothMouse(yaw*dt, 0)

	// Jump every 1.5-3 seconds
	ms := elapsed.Milliseconds()
	if ms > 0 && ms%int64(1500+rand.Intn(1500)) < int64(tickRate.Milliseconds()) {
		b.input.KeyTap(KeySpace)
	}
}

// ─────────────────────── CORNER CHECK ───────────────────────
// Stop, slowly look left ~60°, pause, slowly look right ~120°, then continue

func (b *CS2Bot) initCornerCheck() {
	b.releaseMovement()
	b.turn.yawRate = 0
	b.turn.pitchRate = 0
	b.turn.yawAccum = 0
	b.turn.pitchAccm = 0
}

func (b *CS2Bot) tickCornerCheck(elapsed time.Duration) {
	dur := b.bhvDuration.Seconds()
	t := elapsed.Seconds() / dur // 0..1 normalized progress
	dt := tickRate.Seconds()

	// Phase 1 (0-0.3): look left smoothly
	// Phase 2 (0.3-0.4): pause
	// Phase 3 (0.4-0.8): look right smoothly
	// Phase 4 (0.8-1.0): center back

	var yawSpeed float64
	switch {
	case t < 0.3:
		yawSpeed = -70 // degrees/s left
	case t < 0.4:
		yawSpeed = 0 // pause
	case t < 0.8:
		yawSpeed = 55 // degrees/s right
	default:
		yawSpeed = -20 // drift back to roughly center
	}

	b.smoothMouse(yawSpeed*dt, 0)

	// Stand still during check, then start walking at the end
	if t > 0.85 {
		b.ensureHeld(KeyW)
	}
}

// ─────────────────────── COMBAT ───────────────────────
// Crouch or stand, aim with smooth tracking sweeps, fire controlled bursts.

func (b *CS2Bot) initCombat() {
	// 50% chance to crouch during combat
	if rand.Intn(2) == 0 {
		b.holdKey(KeyCtrlL)
	}

	// Slight forward movement or stationary
	if rand.Intn(3) > 0 {
		b.holdKey(KeyW)
	}

	// Initial aim direction: simulate acquiring a target
	b.turn.yawRate = (20 + rand.Float64()*40) * randSign()
	b.turn.pitchRate = (-8 + rand.Float64()*16) // slight up/down
	b.turn.yawAccum = 0
	b.turn.pitchAccm = 0

	b.burstTicks = 0
	b.burstCool = rand.Intn(15) + 5 // 5-20 ticks before first burst
}

func (b *CS2Bot) tickCombat(elapsed time.Duration) {
	dt := tickRate.Seconds()
	phase := elapsed.Seconds()

	// Smooth aim tracking — oscillates like tracking a moving target
	yaw := b.turn.yawRate * math.Cos(phase*2.5) * 0.6
	pitch := b.turn.pitchRate * math.Sin(phase*1.8) * 0.4
	b.smoothMouse(yaw*dt, pitch*dt)

	// Burst fire logic
	if b.burstTicks > 0 {
		// Currently firing a burst
		if !b.shooting {
			b.input.MouseDown(1)
			b.shooting = true
		}
		b.burstTicks--

		// Recoil compensation: pull down slightly during spray
		b.smoothMouse(0, -0.3*dt*50)

		if b.burstTicks == 0 {
			b.input.MouseUp(1)
			b.shooting = false
			b.burstCool = rand.Intn(20) + 8 // 8-28 ticks cooldown (~160-560ms)
		}
	} else if b.burstCool > 0 {
		b.burstCool--
	} else {
		// Start a new burst: 4-12 ticks (~80-240ms)
		b.burstTicks = rand.Intn(9) + 4
	}
}

// ─────────────────────── REPOSITION ───────────────────────
// Quick strafe + backward movement to a new position, then resume

func (b *CS2Bot) initReposition() {
	dir := rand.Intn(3)
	switch dir {
	case 0: // strafe left + back
		b.holdKey(KeyA)
		b.holdKey(KeyS)
	case 1: // strafe right + back
		b.holdKey(KeyD)
		b.holdKey(KeyS)
	case 2: // just strafe
		if rand.Intn(2) == 0 {
			b.holdKey(KeyA)
		} else {
			b.holdKey(KeyD)
		}
	}

	b.turn.yawRate = (30 + rand.Float64()*50) * randSign()
	b.turn.yawAccum = 0
	b.turn.pitchAccm = 0
}

func (b *CS2Bot) tickReposition(elapsed time.Duration) {
	dt := tickRate.Seconds()
	b.smoothMouse(b.turn.yawRate*dt, 0)

	// Halfway through, switch to forward
	if elapsed > b.bhvDuration/2 {
		b.releaseKey(KeyS)
		b.releaseKey(KeyA)
		b.releaseKey(KeyD)
		b.ensureHeld(KeyW)
	}
}

// ─────────────────────── smooth mouse ───────────────────────
// Accumulates sub-pixel fractional movement and sends integer deltas.
// This prevents the staircase effect of rounding small movements.

func (b *CS2Bot) smoothMouse(dxDeg, dyDeg float64) {
	// Convert degrees to pixels. CS2 default is ~2.5 sensitivity.
	// At 400 DPI equivalent through Xvfb, 1 degree ≈ ~3 pixels.
	const degToPixel = 3.0

	b.turn.yawAccum += dxDeg * degToPixel
	b.turn.pitchAccm += dyDeg * degToPixel

	dx := int(b.turn.yawAccum)
	dy := int(b.turn.pitchAccm)

	if dx != 0 || dy != 0 {
		b.input.MouseMove(dx, dy)
		b.turn.yawAccum -= float64(dx)
		b.turn.pitchAccm -= float64(dy)
	}
}

// ─────────────────────── input helpers ───────────────────────

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

func (b *CS2Bot) ensureHeld(keysym uint) {
	b.mu.Lock()
	held := b.heldKeys[keysym]
	b.mu.Unlock()
	if !held {
		b.holdKey(keysym)
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

// ─────────────────────── focus / setup ───────────────────────
// Aggressively ensure the CS2 window has input focus inside Xvfb.

func (b *CS2Bot) ensureFocus(ctx context.Context) {
	log.Printf("[CS2Bot:%d] Ensuring game focus...", b.display)

	// Warp mouse to center of the 1280x720 Xvfb screen (absolute)
	b.input.WarpAbsolute(640, 360)
	if !sleepCtx(ctx, 300*time.Millisecond) {
		return
	}

	// Click to focus the game window
	b.input.Click(1)
	if !sleepCtx(ctx, 600*time.Millisecond) {
		return
	}

	// Escape to close any overlay (Steam notifications, MOTD)
	b.input.KeyTap(KeyEscape)
	if !sleepCtx(ctx, 600*time.Millisecond) {
		return
	}

	// Click center again to capture the mouse
	b.input.WarpAbsolute(640, 360)
	if !sleepCtx(ctx, 200*time.Millisecond) {
		return
	}
	b.input.Click(1)
	if !sleepCtx(ctx, 500*time.Millisecond) {
		return
	}

	// Escape once more (dismiss menu if we accidentally opened it)
	b.input.KeyTap(KeyEscape)
	if !sleepCtx(ctx, 400*time.Millisecond) {
		return
	}

	// Warp back to center and click to lock cursor into game
	b.input.WarpAbsolute(640, 360)
	if !sleepCtx(ctx, 200*time.Millisecond) {
		return
	}
	b.input.Click(1)
	if !sleepCtx(ctx, 300*time.Millisecond) {
		return
	}

	// Quick W tap to verify game is receiving input
	b.input.KeyDown(KeyW)
	if !sleepCtx(ctx, 200*time.Millisecond) {
		return
	}
	b.input.KeyUp(KeyW)
	if !sleepCtx(ctx, 200*time.Millisecond) {
		return
	}

	log.Printf("[CS2Bot:%d] Focus sequence done", b.display)
}

// ─────────────────────── utilities ───────────────────────

func randSign() float64 {
	if rand.Intn(2) == 0 {
		return -1
	}
	return 1
}

func randDur(minMs, maxMs int) time.Duration {
	return time.Duration(minMs+rand.Intn(maxMs-minMs)) * time.Millisecond
}

// sleepCtx returns false if context was cancelled
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
