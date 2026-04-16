package autoplay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BotPhase int

const (
	PhaseWaitProcess BotPhase = iota
	PhaseWaitWindow
	PhaseWaitMatch
	PhaseAlive
	PhaseDead
	PhaseFreezeTime
	// PhaseWaitMapLoad — карта известна, ждём докачку/выбор команды/выход из freezetime (до 2 min).
	PhaseWaitMapLoad
)

func (p BotPhase) String() string {
	switch p {
	case PhaseWaitProcess:
		return "wait-process"
	case PhaseWaitWindow:
		return "wait-window"
	case PhaseWaitMatch:
		return "wait-match"
	case PhaseAlive:
		return "alive"
	case PhaseDead:
		return "dead"
	case PhaseFreezeTime:
		return "freezetime"
	case PhaseWaitMapLoad:
		return "wait-mapload"
	default:
		return "unknown"
	}
}

type behaviorKind int

const (
	bhvPatrol behaviorKind = iota
	bhvRoam                // random map walk: WASD + smooth yaw (no pure W sprint)
	bhvCornerCheck
	bhvSprint
	bhvCombat
	bhvReposition
)

type turnState struct {
	yawRate   float64
	pitchRate float64
	wm        windMouseState
}

type CS2Bot struct {
	accountID int64 // farm account — маршрутизация GSI по steam_id без перезаписи второго слота
	display   int
	steamID   string
	input     *InputSender
	gsi       *GSIServer

	mu        sync.Mutex
	phase     BotPhase
	lastGSI   *GSIState
	cancel    context.CancelFunc
	runWg     sync.WaitGroup // run() + внутренний tick — ждём перед input.Close при Stop
	shooting  bool
	heldKeys  map[uint]bool
	running   bool
	startedAt time.Time
	kills     int
	deaths    int

	behavior    behaviorKind
	bhvStart    time.Time
	bhvDuration time.Duration
	turn        turnState

	burstTicks int
	burstCool  int

	lastFocus                time.Time
	lastSteamDialogDismissAt time.Time
	lastRematchSignalAt      time.Time
	rematchCh                chan struct{} // GSI: выход в главное меню с карты → повторная очередь
	tickCount                uint64
	consoleLogStart          int64 // file offset at bot start — only parse new lines

	// autoplayLive: true только после 5b + фокуса — до этого крутим YOLO (зрение), но не движение/GSI-поведение (клики матчмейкинга).
	autoplayLive bool

	// After respawn: new random route (GSI does not expose world pos — heuristic roam only).
	needsNavReset bool
	navYawTarget  float64
	navYawSmooth  float64
	navHeadingAt  time.Time
	navStrafeEnd  time.Time
	navStrafeLeft bool
	navUnstickAt  time.Time
	roamPitch     float64
	roamBackTicks int
	// Граф mapnav: config/mapnav/*.json (или встроенный dust2/mirage), маршрут по узлам, спавн ≈ ближайший узел к GSI XZ.
	navRoute       []int
	navRouteStep   int
	navPrevSampleX float64
	navPrevSampleZ float64
	navSampleAt    time.Time
	navMoveBear    float64
	// После спавна / отлипания от угла — только W+мышь, без длительных стрейфов (меньше «ездим боком» вдоль стен).
	roamWallHugYawOnlyUntil time.Time

	// GSI world position (player_position) — used for forward + no-motion unstuck.
	gsiMapX, gsiMapY, gsiMapZ float64
	gsiPosOK                  bool
	gsiFrozenSince            time.Time
	lastUnstickAt             time.Time
	// Vertical velocity from GSI Y (stairs / ramps) — tilts view slightly up/down vs horizon.
	gsiSampleTime  time.Time
	gsiLastSampleY float64
	gsiVertVel     float64
	// Сглаженное отклонение «вверх/вниз» от горизонта (после спавна ~0); тянем к ~0 с разбросом, без ухода в небо.
	horizonPitchLean float64
	// Radar cvars applied at most once via ~ console (repeat opens were toggling ESC flow / in-game menu).
	radarCvarsDone bool
	// Escalating wall escape: stronger yaw/backpedal after repeated stuck samples.
	stuckEscalation int
	// Continuous forward (W) segment — used when GSI omits world position for the local player.
	forwardSegSince time.Time

	// Миникарта со смещением (cl_radar_always_centered  «панорамирует»): резкий скачок XZ = телепорт/респавен.
	radarJumpDX, radarJumpDZ       float64
	radarJumpAt                    time.Time
	radarPanAccumX, radarPanAccumZ float64

	// Vision: center ROI motion centroid (~8–10 Hz), XGetImage in input worker (no Python).
	vision         visionMotion
	lastVisionGrab time.Time
	combatWaveSeed float64

	// Minimap (grayscale): white-arrow bearing + dark wall samples — no reliance on local player dot hue.
	radarNav          radarMinimapNav
	lastRadarScan     time.Time
	radarStuckSamples int
	lastEnemyRGBGrab  time.Time // эвристика RGB (только без YOLO)

	// YOLO (deathmatch): детект + наводка + стрельба; движение — поведения GSI/роум.
	yolo           *YoloClient
	yoloInferTrace func(display int, w, h, ndets int, err error)
	yoloPreview    *YoloPreviewSink
	// Поверх CS2 на том же X11 (VNC); Linux, по умолчанию вкл. — см. overlay_linux.go.
	enemyOverlay      EnemyOverlay
	yoloEmaYawDeg     float64
	yoloEmaPitchDeg   float64
	yoloSmErrPx       float64 // сглаженная ошибка прицела (px), порог для выстрела
	yoloShotHold      int
	yoloBurstCooldown int

	// Mid-match map rotation (DM): last map we adapted to; when GSI name changes, wait for pawn again.
	sessionMapName string
	mapReloadSince time.Time

	// Optional: SFARM_CS2_MEM_CONFIG — world pose / velocity from process memory (Linux).
	memDriver             cs2MemDriver
	memReaderNextTry      time.Time
	memNavActive          bool
	memAt                 time.Time
	memYawRad             float64
	memSpeed2             float64
	navDesiredBear        float64
	memStuckLowSpeedSince time.Time
	lastMemBumpJumpAt     time.Time
	memAnglesOK           bool // eye angles read from memory (for yaw blend)
	memLastVerboseLog     time.Time
	memLastOKLog          time.Time // дроссель логов «read OK» при успешном snapshot
	lastMemGSIDriftLog    time.Time
	memNavHintLogged      bool // logged once: no mem config → GSI+radar only
	// Дроссель лога read error на боте: memDriver пересоздаётся после ошибки, иначе lastErr* в драйвере обнуляются каждый тик.
	memReadErrLastLog time.Time
	memReadErrMsg     string
	// Подряд неудачных snapshot() подряд — только после порога сбрасываем memDriver (иначе ESP теряет драйвер каждый тик).
	memSnapshotFailStreak int
	// Последний опрос snapshot() в pollCS2MemImpl (для WebSocket cs2:mem).
	lastMemPollAt   time.Time
	lastMemPollOK   bool
	lastMemPollErr  string
	lastEspBoxCount int
	memTelemetry    func(display int, ev map[string]interface{})

	// Телеметрия навигации (JSONL): см. navlog.go; по умолчанию файл в os.TempDir().
	navLogF         *os.File
	navLogPathOpen  string
	navLogLastEmit  time.Time
	teleGraphSteer  bool
	teleRadarWall   float64
	teleRadarYawSug float64
	teleRadarOK     bool
	teleRadarAt     time.Time
	teleRadarRw     int
	teleRadarRh     int
}

type BotConfig struct {
	AccountID int64
	Display   int
	SteamID   string
	GSI       *GSIServer
	Yolo    *YoloClient // общий worker Manager; nil — эвристики боя без сети
	// YoloInferTrace логирует живой X11-кадр → worker (например в UI yolo:log).
	YoloInferTrace func(display int, w, h, ndets int, err error)
	// YoloPreview окно OpenCV на DISPLAY песочницы (VNC), см. SFARM_YOLO_PREVIEW.
	YoloPreview *YoloPreviewSink
	// MemTelemetry — каждый тик (~64 Hz): память, ESP-боксы, nav (в desktop → WebSocket cs2:mem).
	MemTelemetry func(display int, ev map[string]interface{})
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
		accountID:      cfg.AccountID,
		display:        cfg.Display,
		steamID:        cfg.SteamID,
		input:          input,
		gsi:            cfg.GSI,
		yolo:           cfg.Yolo,
		yoloInferTrace: cfg.YoloInferTrace,
		yoloPreview:    cfg.YoloPreview,
		memTelemetry:   cfg.MemTelemetry,
		enemyOverlay:   NewEnemyOverlay(cfg.Display),
		phase:          PhaseWaitProcess,
		heldKeys:       make(map[uint]bool),
		yoloSmErrPx:    4000,
		rematchCh:      make(chan struct{}, 1),
	}, nil
}

func (b *CS2Bot) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)
	if b.gsi != nil {
		b.gsi.RegisterAccountHandler(b.accountID, b.steamID, b.onGSIUpdate)
	}
	b.mu.Lock()
	b.running = true
	b.startedAt = time.Now()
	b.mu.Unlock()

	// Snapshot current console log size so we only parse new lines
	if info, err := os.Stat(cs2ConsolePath()); err == nil {
		b.consoleLogStart = info.Size()
	}

	b.runWg.Add(1)
	go func() {
		defer b.runWg.Done()
		b.run(ctx)
	}()
	log.Printf("[CS2Bot] Started account=%d display :%d (steamID=%s) [v2-smart-detect]", b.accountID, b.display, b.steamID)
}

func (b *CS2Bot) Stop() {
	if b.yoloPreview != nil {
		b.yoloPreview.Close()
		b.yoloPreview = nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	// Иначе тик после закрытия X11 даёт GrabCS2RGBRect nil и шумный «нет живого кадра» в yolo:log.
	b.runWg.Wait()
	if b.enemyOverlay != nil {
		b.enemyOverlay.Close()
		b.enemyOverlay = nil
	}
	if b.gsi != nil {
		b.gsi.UnregisterAccountHandler(b.accountID)
	}
	memCollectResetDisplay(b.display)
	b.releaseAll()
	b.mu.Lock()
	b.closeNavLogLocked()
	b.mu.Unlock()
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
	case bhvRoam:
		return "roam"
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
	defer memCollectUpdateFromGSI(b.display, state)
	b.mu.Lock()
	prev := b.lastGSI
	b.lastGSI = state
	defer func() {
		b.maybeScheduleRematchLocked(prev, state)
		b.mu.Unlock()
	}()

	if prev == nil {
		mapName := ""
		if state.Map != nil {
			mapName = state.Map.Name
		}
		log.Printf("[CS2Bot:%d] First GSI update received (map=%s, provider=%s)",
			b.display, mapName, state.Provider.SteamID)
	}

	if state.Player != nil {
		b.ingestGSIPositionLocked(state)
	}

	if state.Player == nil || state.Player.State == nil {
		b.maybeDetectMidMatchMapChangeLocked(prev, state)
	} else {
		wasAlive := prev != nil && prev.Player != nil && prev.Player.State != nil && prev.Player.State.Health > 0
		nowAlive := state.Player.State.Health > 0

		if wasAlive && !nowAlive {
			b.deaths++
			b.phase = PhaseDead
			b.gsiFrozenSince = time.Time{}
			b.stuckEscalation = 0
		} else if nowAlive {
			if !wasAlive {
				b.needsNavReset = true
				// Респавн / первый выход в бой: сдвиг радара по экрану, сглаживание и интервал скана обнуляем.
				b.resetRadarScanStateLocked()
			}
			if state.Round != nil && state.Round.Phase == "freezetime" {
				b.phase = PhaseFreezeTime
			} else {
				b.phase = PhaseAlive
			}
		} else {
			b.phase = PhaseDead
		}

		b.maybeDetectMidMatchMapChangeLocked(prev, state)
	}
}

// maybeDetectMidMatchMapChangeLocked: GSI map name changed while in session → reload phase (caller holds b.mu).
func (b *CS2Bot) maybeDetectMidMatchMapChangeLocked(prev, state *GSIState) {
	if b.sessionMapName == "" {
		return
	}
	cur := gsiMapName(state)
	if cur == "" || cur == b.sessionMapName {
		return
	}
	switch b.phase {
	case PhaseAlive, PhaseDead, PhaseFreezeTime:
	default:
		return
	}
	log.Printf("[CS2Bot:%d] Map changed: %q → %q — waiting for controllable pawn + behavior reset",
		b.display, b.sessionMapName, cur)
	b.phase = PhaseWaitMapLoad
	b.mapReloadSince = time.Now()
	b.resetBehaviorStateForNewMapLocked()
}

// resetBehaviorStateForNewMapLocked clears nav / GSI anchors / vision so logic matches the new map (caller holds b.mu).
func (b *CS2Bot) resetBehaviorStateForNewMapLocked() {
	b.gsiPosOK = false
	b.gsiMapX, b.gsiMapY, b.gsiMapZ = 0, 0, 0
	b.gsiFrozenSince = time.Time{}
	b.gsiVertVel = 0
	b.gsiSampleTime = time.Time{}
	b.gsiLastSampleY = 0
	b.horizonPitchLean = 0
	b.radarJumpDX, b.radarJumpDZ = 0, 0
	b.radarJumpAt = time.Time{}
	b.radarPanAccumX, b.radarPanAccumZ = 0, 0
	b.stuckEscalation = 0
	b.needsNavReset = false
	b.navYawTarget = 0
	b.navYawSmooth = 0
	b.navHeadingAt = time.Time{}
	b.navStrafeEnd = time.Time{}
	b.navStrafeLeft = false
	b.navUnstickAt = time.Time{}
	b.roamPitch = 0
	b.roamBackTicks = 0
	b.roamWallHugYawOnlyUntil = time.Time{}
	b.forwardSegSince = time.Time{}
	b.turn = turnState{}
	b.turn.wm.Reset()
	b.burstTicks = 0
	b.burstCool = 0
	b.vision.Reset()
	b.lastVisionGrab = time.Time{}
	b.combatWaveSeed = 0
	b.shooting = false
	b.lastUnstickAt = time.Time{}
	b.resetRadarScanStateLocked()
	b.lastEnemyRGBGrab = time.Time{}
	b.yoloEmaYawDeg = 0
	b.yoloEmaPitchDeg = 0
	b.yoloSmErrPx = 4000
	b.yoloShotHold = 0
	b.yoloBurstCooldown = 0
	b.navRoute = nil
	b.navRouteStep = 0
	b.navPrevSampleX = 0
	b.navPrevSampleZ = 0
	b.navSampleAt = time.Time{}
	b.navMoveBear = 0
	b.memNavActive = false
	b.memAt = time.Time{}
	b.memYawRad = 0
	b.memSpeed2 = 0
	b.navDesiredBear = 0
	b.memStuckLowSpeedSince = time.Time{}
	b.lastMemBumpJumpAt = time.Time{}
	b.memAnglesOK = false
	b.memLastVerboseLog = time.Time{}
	b.memLastOKLog = time.Time{}
	b.lastMemGSIDriftLog = time.Time{}
	b.memSnapshotFailStreak = 0
}

// resetRadarScanStateLocked — после респавна / телепорта / смены карты HUD радара смещается; сбрасываем EMA стен и форсируем новый захват по таймеру.
func (b *CS2Bot) resetRadarScanStateLocked() {
	b.radarNav.Reset()
	b.lastRadarScan = time.Time{}
	b.radarStuckSamples = 0
	// После респавна камера горизонтальна — задаём небольшой случайный оффсет, не одну фиксированную точку.
	b.horizonPitchLean = (rand.Float64()*2 - 1) * 1.15
}

func gsiMapName(g *GSIState) string {
	if g == nil || g.Map == nil {
		return ""
	}
	return strings.TrimSpace(g.Map.Name)
}

// ingestGSIPositionLocked updates stuck-detection state from GSI "player.position" ("x, y, z").
// Caller must hold b.mu.
func (b *CS2Bot) ingestGSIPositionLocked(state *GSIState) {
	p := state.Player
	if p == nil {
		return
	}
	act := strings.ToLower(strings.TrimSpace(p.Activity))
	if act == "menu" || act == "textinput" {
		b.gsiPosOK = false
		b.gsiFrozenSince = time.Time{}
		b.gsiVertVel = 0
		b.gsiSampleTime = time.Time{}
		b.memNavActive = false
		return
	}
	if b.memNavActive && time.Since(b.memAt) < memNavMaxSteerAge {
		return
	}
	pos := strings.TrimSpace(p.Position)
	if pos == "" {
		return
	}
	x, y, z, ok := parseVec3FromGSI(pos)
	if !ok {
		return
	}

	now := time.Now()
	if !b.gsiSampleTime.IsZero() {
		dt := now.Sub(b.gsiSampleTime).Seconds()
		if dt > 0.04 {
			vy := (y - b.gsiLastSampleY) / dt
			b.gsiVertVel = 0.55*b.gsiVertVel + 0.45*vy
		}
	}
	b.gsiSampleTime = now
	b.gsiLastSampleY = y
	b.ingestWorldXZLocked(x, y, z, now)
}

// ingestWorldXZLocked: телепорт / застревание по сэмплам мира (GSI или память). Caller holds b.mu.
func (b *CS2Bot) ingestWorldXZLocked(x, y, z float64, now time.Time) {
	if !b.gsiPosOK {
		b.gsiMapX, b.gsiMapY, b.gsiMapZ = x, y, z
		b.gsiPosOK = true
		b.gsiFrozenSince = time.Time{}
		b.stuckEscalation = 0
		return
	}
	dx := x - b.gsiMapX
	dz := z - b.gsiMapZ
	jSq := dx*dx + dz*dz
	if jSq >= gsiMinimapJumpSq {
		b.radarJumpDX, b.radarJumpDZ = dx, dz
		b.radarJumpAt = now
		b.radarPanAccumX += dx
		b.radarPanAccumZ += dz
		log.Printf("[CS2Bot:%d] Minimap anchor jump Δx=%.0f Δz=%.0f (accum Δx=%.0f Δz=%.0f) — respawn/teleport",
			b.display, dx, dz, b.radarPanAccumX, b.radarPanAccumZ)
		b.gsiMapX, b.gsiMapY, b.gsiMapZ = x, y, z
		b.gsiFrozenSince = time.Time{}
		b.stuckEscalation = 0
		b.needsNavReset = true
		b.resetRadarScanStateLocked()
		return
	}
	if jSq >= gsiStuckMoveSq {
		b.gsiMapX, b.gsiMapY, b.gsiMapZ = x, y, z
		b.gsiFrozenSince = time.Time{}
		b.stuckEscalation = 0
		return
	}
	if b.gsiFrozenSince.IsZero() {
		b.gsiFrozenSince = now
	}
}

func parseVec3FromGSI(s string) (x, y, z float64, ok bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	var err error
	x, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, 0, false
	}
	y, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, 0, false
	}
	z, err = strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return x, y, z, true
}

// ─────────────────────── main loop ───────────────────────

// cs2BotTickInterval drives the bot loop, memory poll, and YOLO/overlay cadence.
// По умолчанию: 64 Hz если SFARM_CS2_LOW_CPU=0; иначе 32 Hz (меньше нагрузка при нескольких аккаунтах). Явно: SFARM_CS2_BOT_TICK_HZ=48
var cs2BotTickInterval = time.Second / 64

func init() {
	hz := defaultBotTickHz()
	if s := strings.TrimSpace(os.Getenv("SFARM_CS2_BOT_TICK_HZ")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 10 && v <= 500 {
			hz = v
		} else {
			log.Printf("[CS2Bot] SFARM_CS2_BOT_TICK_HZ=%q invalid (use 10..500), using default %d", s, defaultBotTickHz())
			hz = defaultBotTickHz()
		}
	}
	cs2BotTickInterval = time.Second / time.Duration(hz)
	if hz != 64 {
		log.Printf("[CS2Bot] bot loop ~%d Hz (%v/tick)", hz, cs2BotTickInterval)
	}
}

// Radar CVars applied via console (and mirrored in launch opts). North-up, zoomed out HUD radar.
const cs2RadarConsoleLine = "cl_radar_always_centered 0; cl_radar_rotate 0; cl_radar_scale 0.30; cl_hud_radar_scale 1.15; cl_radar_icon_scale_min 0.45; cl_radar_icon_scale_max 0.75; cl_radar_square_with_outline 1; cl_hud_radar_map_additive 0; cl_hud_radar_blur_background 1; cl_hud_radar_background_alpha 1"

// Память для roam: слишком узкое окно (220ms) отрезало коррекцию курса при редких удачных read.
const memNavMaxSteerAge = 480 * time.Millisecond
const memNavFreshAge = 110 * time.Millisecond

// sq(float64) — compare movement on XZ between GSI samples (~0.5s); below this = “not moving”.
const gsiStuckMoveSq = 20.0 * 20.0

// Резкий перенос по карте за один тик GSI — новый «якорь» миникарты (респавен / телепорт).
const gsiMinimapJumpSq = 280.0 * 280.0

const gsiStuckHoldAfter = 700 * time.Millisecond
const unstuckCooldown = 320 * time.Millisecond

// If GSI never sends player_position, wall contact still stops movement — unstick on long W holds.
const forwardStuckNoGSIPos = 2300 * time.Millisecond
const forwardStuckLongPush = 3800 * time.Millisecond // backup when GSI reports tiny drift but view is on a wall

// cs2JoinSkipDeathmatchModeClick: do not click the DEATHMATCH tile in Play — reuse the checklist
// already saved in CS2 (Panorama). Re-clicking the mode row often resets pool / enables “all maps”.
// Disable if you rely on the bot to force Deathmatch from a cold Play tab (e.g. wrong default mode).
const cs2JoinSkipDeathmatchModeClick = true

func (b *CS2Bot) run(ctx context.Context) {
	runCtx, cancelRun := context.WithCancel(ctx)
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		ticker := time.NewTicker(cs2BotTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				b.tick(runCtx)
			}
		}
	}()
	defer func() {
		cancelRun()
		<-tickDone
	}()

	b.mu.Lock()
	b.autoplayLive = false
	b.mu.Unlock()

	// Phase 1: Wait for CS2 process
	if !b.waitForCS2Process(ctx) {
		return
	}

	// Phase 2: Wait for CS2 to reach main menu (shaders compiled, UI loaded)
	if !b.waitForMainMenu(ctx) {
		return
	}

	// Phase 3: Focus CS2 window, dismiss dialogs
	b.ensureFocus(ctx)

	// Phase 4: Queue for online Deathmatch via console + UI
	b.joinMatchmaking(ctx)

	// Phase 5a: очередь + появление имени карты на сервере
	if !b.waitForMatch(ctx) {
		return
	}
	// Phase 5b: до 2 min — прогрузка, выбор команды, freezetime → управляемый персонаж
	if !b.waitForPlayablePawn(ctx) {
		return
	}

	// Phase 6: final focus before inputs (no extra radar console — avoids ~ spam in match)
	b.ensureFocus(ctx)

	b.mu.Lock()
	if b.phase != PhaseAlive && b.phase != PhaseDead {
		b.phase = PhaseAlive
	}
	if b.lastGSI != nil && b.lastGSI.Map != nil {
		b.sessionMapName = strings.TrimSpace(b.lastGSI.Map.Name)
	}
	b.autoplayLive = true
	b.mu.Unlock()

	log.Printf("[CS2Bot:%d] All systems go — starting autoplay (session map=%q)", b.display, b.sessionMapName)
	b.pickBehavior()

	for {
		select {
		case <-ctx.Done():
			b.releaseAll()
			return
		case <-b.rematchCh:
			b.handleDisconnectRematch(ctx)
		}
	}
}

// ─────────────────── smart game detection ────────────────────

func (b *CS2Bot) waitForCS2Process(ctx context.Context) bool {
	b.setPhase(PhaseWaitProcess)
	log.Printf("[CS2Bot:%d] Phase 1: waiting for cs2 process on :%d + matching window...", b.display, b.display)

	deadline := time.After(10 * time.Minute)
	iter := 0
	for {
		win := b.input.HasCS2Window()
		proc := isCS2RunningOnDisplay(b.display, b.accountID)
		if win && proc {
			log.Printf("[CS2Bot:%d] CS2 process (DISPLAY=:%d) + window OK", b.display, b.display)
			return true
		}
		iter++
		if iter%6 == 0 {
			log.Printf("[CS2Bot:%d] Phase 1: window=%v cs2_on_display_%d=%v (need both)",
				b.display, win, b.display, proc)
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			log.Printf("[CS2Bot:%d] Timeout waiting for CS2 process+window (10min)", b.display)
			return false
		default:
		}
		if !sleepCtx(ctx, 5*time.Second) {
			return false
		}
	}
}

// waitForMainMenu waits for CS2 to finish shader compilation and reach main menu.
// Watches console.log for CSGO_GAME_UI_STATE_MAINMENU signal (written after shaders).
// Also monitors file growth to detect CS2 is alive even during long shader compilation.
func (b *CS2Bot) waitForMainMenu(ctx context.Context) bool {
	b.setPhase(PhaseWaitWindow)
	log.Printf("[CS2Bot:%d] Phase 2: waiting for CS2 main menu (shaders may take minutes); console.log=%s", b.display, cs2ConsolePath())

	// Reset consoleLogStart to current file size so we only detect
	// FRESH signals written after this bot run started.
	if info, err := os.Stat(cs2ConsolePath()); err == nil {
		b.consoleLogStart = info.Size()
		log.Printf("[CS2Bot:%d] Console log offset reset to %d bytes (ignoring stale data)", b.display, b.consoleLogStart)
	}

	startedAt := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastLogSize int64

	for {
		elapsed := time.Since(startedAt)

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			signal := b.checkConsoleLog()

			if signal == "main_menu" || signal == "network_ready" {
				log.Printf("[CS2Bot:%d] Console: %s (%.0fs) — main menu reached!", b.display, signal, elapsed.Seconds())
				sleepCtx(ctx, 10*time.Second)
				return true
			}

			if signal == "map_loaded" || signal == "round_start" || signal == "spawned" {
				log.Printf("[CS2Bot:%d] Console: %s (%.0fs) — already in match!", b.display, signal, elapsed.Seconds())
				return true
			}

			if b.hasGSIMap() {
				log.Printf("[CS2Bot:%d] GSI active (%.0fs) — CS2 loaded", b.display, elapsed.Seconds())
				return true
			}

			if b.gsiAtMainMenu() {
				log.Printf("[CS2Bot:%d] GSI: main menu (%.0fs) — UI ready", b.display, elapsed.Seconds())
				sleepCtx(ctx, 5*time.Second)
				return true
			}

			// Track console.log size to detect CS2 activity (shaders, loading)
			curSize := b.consoleLogSize()
			logGrowing := curSize > lastLogSize && curSize > 0
			if logGrowing {
				log.Printf("[CS2Bot:%d] Waiting... (%.0fs) console.log growing (%d bytes)", b.display, elapsed.Seconds(), curSize)
				lastLogSize = curSize
			} else {
				log.Printf("[CS2Bot:%d] Waiting... (%.0fs) no console activity", b.display, elapsed.Seconds())
			}

			// Hard timeout: 10 min, but only if log is NOT growing.
			// If log is growing, CS2 is alive — keep waiting.
			if elapsed > 10*time.Minute && !logGrowing {
				log.Printf("[CS2Bot:%d] 10min timeout, no activity — proceeding anyway", b.display)
				return true
			}
		}
	}
}

// ─────────────────── matchmaking ────────────────────

// takeScreenshot saves a diagnostic screenshot to /tmp for debugging.
func (b *CS2Bot) takeScreenshot(label string) {
	dispStr := fmt.Sprintf(":%d", b.display)
	ts := time.Now().Format("150405")
	xwdPath := fmt.Sprintf("/tmp/cs2_diag_%s_%s.xwd", ts, label)
	pngPath := fmt.Sprintf("/tmp/cs2_diag_%s_%s.png", ts, label)

	cmd := exec.Command("xwd", "-root", "-display", dispStr, "-out", xwdPath)
	if err := cmd.Run(); err != nil {
		log.Printf("[CS2Bot:%d] Screenshot %s failed: %v", b.display, label, err)
		return
	}

	conv := exec.Command("python3", "-c", fmt.Sprintf(`
import struct
from PIL import Image
with open(%q, 'rb') as f:
    data = f.read()
hs = struct.unpack('>I', data[0:4])[0]
w = struct.unpack('>I', data[16:20])[0]
h = struct.unpack('>I', data[20:24])[0]
img = Image.frombytes('RGBX', (w, h), data[hs:], 'raw', 'BGRX').convert('RGB')
img.save(%q, 'PNG')
`, xwdPath, pngPath))
	if err := conv.Run(); err != nil {
		log.Printf("[CS2Bot:%d] Screenshot convert %s failed: %v", b.display, label, err)
		return
	}
	os.Remove(xwdPath)
	log.Printf("[CS2Bot:%d] Screenshot saved: %s", b.display, pngPath)
}

// menuRef* / menuOut* — CS2 Panorama coordinates measured on real 1024×598 VNC captures,
// scaled to Xvfb 1280×720 (sandbox-core Xvfb -screen 0 1280x720x24).
func menuCoord(refX, refY int) (int, int) {
	const refW, refH = 1024, 598
	const outW, outH = 1280, 720
	return refX * outW / refW, refY * outH / refH
}

// applyRadarConsoleOnce opens ~ once per bot lifetime, applies cvars, closes ~ again — never Escape (Escape was opening pause/Tab UI after console).
func (b *CS2Bot) applyRadarConsoleOnce(ctx context.Context, when string) {
	b.mu.Lock()
	already := b.radarCvarsDone
	b.mu.Unlock()
	if already {
		return
	}
	log.Printf("[CS2Bot:%d] Radar console (%s, once)...", b.display, when)
	b.input.FocusGame()
	if !sleepCtx(ctx, 250*time.Millisecond) {
		return
	}
	b.input.KeyTap(KeyTilde)
	if !sleepCtx(ctx, 300*time.Millisecond) {
		return
	}
	b.input.TypeLine(cs2RadarConsoleLine)
	if !sleepCtx(ctx, 150*time.Millisecond) {
		return
	}
	b.input.KeyTap(KeyTilde)
	if !sleepCtx(ctx, 200*time.Millisecond) {
		return
	}
	b.mu.Lock()
	b.radarCvarsDone = true
	b.mu.Unlock()
}

// stabilizeUI refreshes window cache + focus after console typing or X11 reconnect so clicks hit CS2.
func (b *CS2Bot) stabilizeUI(ctx context.Context) {
	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	sleepCtx(ctx, 750*time.Millisecond)
}

// joinMatchmaking opens Play and queues with GO. Map checklist = player’s saved Play settings
// (no clicks in the map list). Optionally skips re-selecting DEATHMATCH — see cs2JoinSkipDeathmatchModeClick.
// Uses atomic ClickAt (move+hold+release in C). Coordinates = menuCoord(ref baseline 1024×598).
func (b *CS2Bot) joinMatchmaking(ctx context.Context) {
	b.setPhase(PhaseWaitMatch)
	log.Printf("[CS2Bot:%d] Phase 4: joining Deathmatch matchmaking...", b.display)

	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	if !sleepCtx(ctx, 800*time.Millisecond) {
		return
	}
	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	if !sleepCtx(ctx, 800*time.Millisecond) {
		return
	}

	// Radar: single console pass; do not send Escape here — it leaves PLAY/DM Panorama and breaks GO.
	b.applyRadarConsoleOnce(ctx, "pre-match")
	b.stabilizeUI(ctx)

	b.takeScreenshot("1_before_play")

	b.clickPlayTabMulti(ctx)
	if !sleepCtx(ctx, 400*time.Millisecond) {
		return
	}

	b.takeScreenshot("2_after_play")

	// Step 2: DEATHMATCH mode (optional). Skipped by default so CS2 keeps the user’s map pool.
	if !cs2JoinSkipDeathmatchModeClick {
		b.stabilizeUI(ctx)
		dmx, dmy := menuCoord(515, 148)
		log.Printf("[CS2Bot:%d] Step 2: DEATHMATCH ref(515,148) → (%d,%d)", b.display, dmx, dmy)
		b.input.ClickAt(dmx, dmy, 1)
		if !sleepCtx(ctx, 1500*time.Millisecond) {
			return
		}
	} else {
		log.Printf("[CS2Bot:%d] Step 2: skip DEATHMATCH click — using saved Play maps/mode", b.display)
		if !sleepCtx(ctx, 400*time.Millisecond) {
			return
		}
	}

	b.takeScreenshot("3_after_dm")

	b.stabilizeUI(ctx)
	// Step 3: GO (bottom-right). No clicks on map rows — checklist is unchanged.
	gx, gy := menuCoord(857, 571)
	log.Printf("[CS2Bot:%d] Step 3: GO ref(857,571) → (%d,%d)", b.display, gx, gy)
	b.input.ClickAt(gx, gy, 1)
	if !sleepCtx(ctx, 1200*time.Millisecond) {
		return
	}

	b.takeScreenshot("4_after_go")

	log.Printf("[CS2Bot:%d] Matchmaking clicks sent — waiting for server...", b.display)
}

// waitForMatch (5a) ждёт подключения к серверу и появления имени карты в GSI. Спавн и команда — в waitForPlayablePawn.
func (b *CS2Bot) waitForMatch(ctx context.Context) bool {
	log.Printf("[CS2Bot:%d] Phase 5a: waiting for server + map name...", b.display)

	deadline := time.After(3 * time.Minute) // DM queue can take up to ~3min
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	retryAt := time.Now().Add(90 * time.Second) // retry matchmaking clicks after 90s

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			log.Printf("[CS2Bot:%d] Match wait timeout (3min) — giving up", b.display)
			return false
		case <-ticker.C:
			b.mu.Lock()
			gsi := b.lastGSI
			b.mu.Unlock()

			if gsi != nil && gsi.Map != nil && gsi.Map.Name != "" {
				log.Printf("[CS2Bot:%d] GSI: map loaded — %s (Phase 5a done, next: spawn wait)", b.display, gsi.Map.Name)
				return true
			}

			signal := b.checkConsoleLog()
			if signal == "connected" {
				log.Printf("[CS2Bot:%d] Console: connected to server, waiting for map name...", b.display)
			}

			if time.Now().After(retryAt) {
				b.mu.Lock()
				gLate := b.lastGSI
				b.mu.Unlock()
				if gLate != nil && gLate.Map != nil && strings.TrimSpace(gLate.Map.Name) != "" {
					log.Printf("[CS2Bot:%d] GSI map=%q before queue retry — skipping Escape (avoid pause/loadout in match)",
						b.display, strings.TrimSpace(gLate.Map.Name))
					retryAt = time.Now().Add(90 * time.Second)
					continue
				}
				log.Printf("[CS2Bot:%d] Retrying matchmaking queue (no map in GSI yet)...", b.display)
				b.input.InvalidateWindowCache()
				b.input.FocusGame()
				sleepCtx(ctx, 500*time.Millisecond)
				b.input.KeyTap(KeyEscape)
				sleepCtx(ctx, 450*time.Millisecond)
				b.joinMatchmaking(ctx)
				retryAt = time.Now().Add(90 * time.Second)
			}
			// Avoid Enter here — it can confirm Panorama dialogs / map-pool toggles while searching.
		}
	}
}

// waitForPlayablePawn (5b) после известной карты: до 2 min прогрузка / выбор стороны / конец заморозки, затем автоплей.
func (b *CS2Bot) waitForPlayablePawn(ctx context.Context) bool {
	b.setPhase(PhaseWaitMapLoad)
	log.Printf("[CS2Bot:%d] Phase 5b: load + team + controllable pawn (max 2m, radar scan logic after)...", b.display)

	deadline := time.Now().Add(2 * time.Minute)
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			sig := b.checkConsoleLog()
			if sig == "spawned" || sig == "round_start" {
				log.Printf("[CS2Bot:%d] Phase 5b: console %s — pawn ready", b.display, sig)
				return true
			}

			b.mu.Lock()
			g := b.lastGSI
			b.mu.Unlock()

			if gsiPawnControllable(g) {
				rp := ""
				if g.Round != nil {
					rp = g.Round.Phase
				}
				log.Printf("[CS2Bot:%d] Phase 5b: GSI controllable pawn hp=%d round=%q map=%s",
					b.display, g.Player.State.Health, rp, g.Map.Name)
				return true
			}

			if time.Now().After(deadline) {
				log.Printf("[CS2Bot:%d] Phase 5b: 2m timeout — continuing autoplay (pawn may still be loading)", b.display)
				return true
			}

			b.input.KeyTap(KeyReturn)
		}
	}
}

func (b *CS2Bot) setPhase(p BotPhase) {
	b.mu.Lock()
	b.phase = p
	b.mu.Unlock()
}

// syncForwardHoldClock maintains forwardSegSince for continuous W (resets when W released).
func (b *CS2Bot) syncForwardHoldClock() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.heldKeys[KeyW] {
		if b.forwardSegSince.IsZero() {
			b.forwardSegSince = time.Now()
		}
	} else {
		b.forwardSegSince = time.Time{}
	}
}

// maybeUnstickForward: wall escape from (1) GSI XZ freeze, (2) long W when CS2 omits position, (3) long W in roam with GSI micro-drift.
func (b *CS2Bot) maybeUnstickForward() {
	b.mu.Lock()
	forward := b.heldKeys[KeyW]
	frozen := b.gsiFrozenSince
	posOK := b.gsiPosOK
	lastU := b.lastUnstickAt
	var segAge time.Duration
	if forward && !b.forwardSegSince.IsZero() {
		segAge = time.Since(b.forwardSegSince)
	}
	bhv := b.behavior
	b.mu.Unlock()

	if !forward || time.Since(lastU) < unstuckCooldown {
		return
	}

	gsiStuck := posOK && !frozen.IsZero() && time.Since(frozen) >= gsiStuckHoldAfter
	noPosStuck := !posOK && segAge >= forwardStuckNoGSIPos
	longPush := posOK && (bhv == bhvRoam || bhv == bhvPatrol || bhv == bhvSprint) && segAge >= forwardStuckLongPush

	if !gsiStuck && !noPosStuck && !longPush {
		return
	}

	b.mu.Lock()
	esc := b.stuckEscalation
	if esc > 4 {
		esc = 4
	}
	b.stuckEscalation = esc + 1
	b.forwardSegSince = time.Time{}
	b.mu.Unlock()

	b.doUnstickMotion(esc)
	b.mu.Lock()
	b.lastUnstickAt = time.Now()
	b.gsiFrozenSince = time.Time{}
	b.roamWallHugYawOnlyUntil = time.Now().Add(randDur(1200, 2600))
	b.mu.Unlock()
}

// doUnstickMotion: стрейф + рывок yaw, затем вперёд + прыжок (без S — назад+прыжок плохо обходят угол).
func (b *CS2Bot) doUnstickMotion(esc int) {
	b.releaseKey(KeyW)
	b.releaseKey(KeyS)

	var side uint = KeyA
	if rand.Intn(2) == 0 {
		side = KeyD
	}
	b.input.KeyDown(side)

	yaw := 380 + rand.Intn(420)
	if esc >= 2 {
		yaw = 720 + rand.Intn(600)
	}
	if esc >= 4 {
		yaw = 1100 + rand.Intn(500)
	}
	if rand.Intn(2) == 0 {
		yaw = -yaw
	}
	// Положительный dy — слегка вниз к горизонту (отрицательный уводил в небо).
	lookDn := 38 + rand.Intn(55)
	b.input.MouseMove(yaw, lookDn)
	time.Sleep(160 * time.Millisecond)
	b.input.KeyUp(side)
	b.input.MouseMove(rand.Intn(260)-130, 14+rand.Intn(24))
	time.Sleep(90 * time.Millisecond)

	b.input.KeyDown(KeyW)
	time.Sleep(40 * time.Millisecond)
	for i := 0; i < 2; i++ {
		b.input.KeyTap(KeySpace)
		time.Sleep(75 * time.Millisecond)
	}
	b.input.KeyUp(KeyW)
}

// clickPlayTabMulti opens the Play panel with a short sweep — wide multi-click was jostling adjacent Panorama.
func (b *CS2Bot) clickPlayTabMulti(ctx context.Context) {
	refs := []struct{ x, y int }{
		{521, 37}, {528, 33}, {515, 39},
	}
	for i, r := range refs {
		px, py := menuCoord(r.x, r.y)
		log.Printf("[CS2Bot:%d] PLAY tap %d/%d ref(%d,%d) → (%d,%d)", b.display, i+1, len(refs), r.x, r.y, px, py)
		b.input.ClickAt(px, py, 1)
		if !sleepCtx(ctx, 320*time.Millisecond) {
			return
		}
	}
	b.stabilizeUI(ctx)
	px, py := menuCoord(521, 37)
	log.Printf("[CS2Bot:%d] PLAY confirm ref(521,37) → (%d,%d)", b.display, px, py)
	b.input.ClickAt(px, py, 1)
	if !sleepCtx(ctx, 1400*time.Millisecond) {
		return
	}
}

func (b *CS2Bot) hasGSIMap() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastGSI != nil && b.lastGSI.Map != nil && b.lastGSI.Map.Name != ""
}

// gsiAtMainMenu returns true when GSI reports the Panorama main menu.
func (b *CS2Bot) gsiAtMainMenu() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return gsiAtMainMenuState(b.lastGSI)
}

func gsiAtMainMenuState(g *GSIState) bool {
	if g == nil || g.Player == nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(g.Player.Activity)) != "menu" {
		return false
	}
	if g.Map != nil && strings.TrimSpace(g.Map.Name) != "" {
		return false
	}
	return true
}

// gsiDroppedFromMatch: был playable сессия на карте, новый снимок — выход в главное меню / обрыв.
func gsiDroppedFromMatch(prev, cur *GSIState) bool {
	if prev == nil || cur == nil {
		return false
	}
	if gsiMapName(prev) == "" {
		return false
	}
	if prev.Player == nil {
		return false
	}
	if !gsiActivityInWorld(prev) {
		return false
	}
	if gsiAtMainMenuState(cur) {
		return true
	}
	if cur.Player == nil {
		return gsiMapName(cur) == ""
	}
	if cur.Player.State == nil {
		return gsiMapName(cur) == ""
	}
	return false
}

func cs2AutoRematchEnabled() bool {
	return strings.TrimSpace(os.Getenv("SFARM_CS2_AUTO_REMATCH")) != "0"
}

func (b *CS2Bot) maybeScheduleRematchLocked(prev, cur *GSIState) {
	if !cs2AutoRematchEnabled() {
		return
	}
	if !b.autoplayLive || b.sessionMapName == "" {
		return
	}
	if !gsiDroppedFromMatch(prev, cur) {
		return
	}
	if time.Since(b.lastRematchSignalAt) < 20*time.Second {
		return
	}
	select {
	case b.rematchCh <- struct{}{}:
		b.lastRematchSignalAt = time.Now()
		log.Printf("[CS2Bot:%d] Session ended (GSI) — scheduling re-queue", b.display)
	default:
	}
}

func (b *CS2Bot) dismissPanoramaPopups(ctx context.Context) {
	for range 5 {
		b.input.InvalidateWindowCache()
		b.input.FocusGame()
		if !sleepCtx(ctx, 160*time.Millisecond) {
			return
		}
		b.input.KeyTap(KeyReturn)
		if !sleepCtx(ctx, 140*time.Millisecond) {
			return
		}
	}
}

// handleDisconnectRematch closes blocking Steam/CS2 UI, then repeats join → wait match → wait pawn.
func (b *CS2Bot) handleDisconnectRematch(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	log.Printf("[CS2Bot:%d] Re-queue after disconnect/kick...", b.display)

	b.mu.Lock()
	b.autoplayLive = false
	b.phase = PhaseWaitMatch
	b.sessionMapName = ""
	b.mapReloadSince = time.Time{}
	b.mu.Unlock()

	memCollectResetDisplay(b.display)

	matchOK := false
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return
		}
		_ = dismissSteamLikeDialogs(b.display, &b.lastSteamDialogDismissAt, true)
		b.dismissPanoramaPopups(ctx)
		if !sleepCtx(ctx, 400*time.Millisecond) {
			return
		}
		b.ensureFocus(ctx)
		_ = dismissSteamLikeDialogs(b.display, &b.lastSteamDialogDismissAt, true)
		b.dismissPanoramaPopups(ctx)

		b.joinMatchmaking(ctx)
		if b.waitForMatch(ctx) {
			matchOK = true
			break
		}
		log.Printf("[CS2Bot:%d] Re-queue attempt %d/3: waitForMatch failed — retry", b.display, attempt)
		if !sleepCtx(ctx, 22*time.Second) {
			return
		}
	}
	if !matchOK {
		log.Printf("[CS2Bot:%d] Re-queue aborted after 3 waitForMatch failures", b.display)
		return
	}
	if !b.waitForPlayablePawn(ctx) {
		log.Printf("[CS2Bot:%d] Re-queue: waitForPlayablePawn failed", b.display)
		return
	}
	b.ensureFocus(ctx)

	b.mu.Lock()
	if b.phase != PhaseAlive && b.phase != PhaseDead {
		b.phase = PhaseAlive
	}
	if b.lastGSI != nil && b.lastGSI.Map != nil {
		b.sessionMapName = strings.TrimSpace(b.lastGSI.Map.Name)
	}
	b.autoplayLive = true
	b.mu.Unlock()

	log.Printf("[CS2Bot:%d] Re-queue OK — autoplay map=%q", b.display, b.sessionMapName)
	b.pickBehavior()
}

// ─────────────────── console log parsing ────────────────────

func (b *CS2Bot) consoleLogSize() int64 {
	info, err := os.Stat(cs2ConsolePath())
	if err != nil {
		return 0
	}
	return info.Size()
}

// Путь к console.log относительно корня Steam (.../.local/share/Steam).
func cs2ConsoleRelativeSteam() string {
	return filepath.Join("steamapps", "common", "Counter-Strike Global Offensive", "game", "csgo", "console.log")
}

// sfarm-desktop часто идёт от root, а CS2 пишет лог в $farmuser/snap/... — без этого Phase 2
// вечно «no console activity» и бот не доходит до матчмейкинга.
func cs2ConsolePath() string {
	if p := strings.TrimSpace(os.Getenv("SFARM_CS2_CONSOLE_LOG")); p != "" {
		return p
	}
	if root := strings.TrimSpace(os.Getenv("SFARM_STEAM_ROOT")); root != "" {
		return filepath.Join(root, cs2ConsoleRelativeSteam())
	}
	home, _ := os.UserHomeDir()
	if home == "/root" {
		glob := "/home/*/snap/steam/common/.local/share/Steam/steamapps/common/Counter-Strike Global Offensive/game/csgo/console.log"
		if matches, err := filepath.Glob(glob); err == nil && len(matches) > 0 {
			var best string
			var bestM time.Time
			for _, m := range matches {
				if st, err := os.Stat(m); err == nil && !st.IsDir() {
					if best == "" || st.ModTime().After(bestM) {
						best, bestM = m, st.ModTime()
					}
				}
			}
			if best != "" {
				return best
			}
		}
		farm := filepath.Join("/home/steam-farm/snap/steam/common/.local/share/Steam", cs2ConsoleRelativeSteam())
		if _, err := os.Stat(farm); err == nil {
			return farm
		}
	}
	return filepath.Join(home, "snap/steam/common/.local/share/Steam", cs2ConsoleRelativeSteam())
}

// cs2CfgDir — каталог cfg (gamestate_integration_*.cfg); тот же резолв корня Steam, что и console.log.
func cs2CfgDir() string {
	return filepath.Join(filepath.Dir(cs2ConsolePath()), "cfg")
}

// checkConsoleLog reads new lines from the CS2 console log and returns
// the most significant game-state signal found (empty if none).
// Signals in priority order: "spawned", "round_start", "map_loaded", "connected".
func (b *CS2Bot) checkConsoleLog() string {
	f, err := os.Open(cs2ConsolePath())
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	if info.Size() < b.consoleLogStart {
		b.consoleLogStart = 0
	}
	if b.consoleLogStart > 0 {
		f.Seek(b.consoleLogStart, 0)
	}

	bestSignal := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.Contains(line, "CSGO_GAME_UI_STATE_MAINMENU"):
			if bestSignal == "" {
				bestSignal = "main_menu"
			}
		case strings.Contains(line, "SDR RelayNetworkStatus") && strings.Contains(line, "avail=OK"):
			if bestSignal == "" || bestSignal == "main_menu" {
				bestSignal = "network_ready"
			}
		case strings.Contains(line, "ChangeLevel") || strings.Contains(line, "Host_NewGame"):
			bestSignal = "map_loaded"
		case strings.Contains(line, "round_start"):
			bestSignal = "round_start"
		case strings.Contains(line, "PlayerSpawn") || strings.Contains(line, "player_spawn"):
			bestSignal = "spawned"
		case strings.Contains(line, "Connected to") && !strings.Contains(line, "loopback"):
			bestSignal = "connected"
		}
	}
	if err := scanner.Err(); err != nil {
		return bestSignal
	}
	if fi, err2 := f.Stat(); err2 == nil {
		b.consoleLogStart = fi.Size()
	}
	return bestSignal
}

// ─────────────────── process detection ────────────────────

func isCS2Running() bool {
	return isCS2RunningOnDisplay(-1, 0)
}

// isCS2RunningOnDisplay: при sandboxAccountID>0 — приоритет sfarm-{id} в environ/maps (и предки);
// иначе DISPLAY/X11. display<0 — любой cs2 (legacy).
func isCS2RunningOnDisplay(display int, sandboxAccountID int64) bool {
	if sandboxAccountID > 0 {
		if _, ok := sandboxReportedCS2PIDAlive(sandboxAccountID); ok {
			return true
		}
	}
	pids := cs2PIDsLinux()
	if len(pids) == 0 {
		return false
	}
	if display < 0 {
		return true
	}
	if sandboxAccountID > 0 {
		for _, pidStr := range pids {
			pid, err := strconv.Atoi(pidStr)
			if err != nil || pid <= 0 {
				continue
			}
			if pidBelongsToSandboxAccount(pid, sandboxAccountID) {
				return true
			}
		}
	}
	for _, pidStr := range pids {
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		if pidAssociatesWithDisplay(pid, display) {
			return true
		}
	}
	return false
}

// procIsCS2FromPgrepF: только для pgrep -f — отбрасываем bash/sh/snap; pgrep -x cs2 не фильтруем (ниже).
func procIsCS2FromPgrepF(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(data))
	if comm == "bash" || comm == "sh" || comm == "dash" || comm == "snap" {
		return false
	}
	if comm == "cs2" {
		return true
	}
	exePath := filepath.Join("/proc", strconv.Itoa(pid), "exe")
	exe, err := os.Readlink(exePath)
	if err != nil {
		// чужой uid: exe часто недоступен; не считаем кандидатом по -f (настоящий cs2 даёт pgrep -x cs2).
		return false
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	return strings.Contains(exe, "linuxsteamrt64/cs2") || strings.HasSuffix(exe, "/cs2")
}

func cs2PIDsLinux() []string {
	seen := make(map[string]struct{})
	// Точное имя процесса — доверяем без /proc/PID/exe (у root/cs2 readlink с другого uid часто EPERM).
	if out, err := exec.Command("pgrep", "-x", "cs2").Output(); err == nil {
		for _, field := range strings.Fields(string(out)) {
			if field != "" {
				seen[field] = struct{}{}
			}
		}
	}
	if out, err := exec.Command("pgrep", "-f", "linuxsteamrt64/cs2").Output(); err == nil {
		for _, field := range strings.Fields(string(out)) {
			if field == "" {
				continue
			}
			if _, ok := seen[field]; ok {
				continue
			}
			p, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			if procIsCS2FromPgrepF(p) {
				seen[field] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for pid := range seen {
		if _, err := strconv.Atoi(pid); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

// ─────────────────────── tick loop ───────────────────────

func (b *CS2Bot) tick(ctx context.Context) {
	b.tickCount++
	if ctx.Err() != nil {
		return
	}

	if time.Since(b.lastFocus) > focusRefreshInterval() {
		b.input.FocusGame()
		b.lastFocus = time.Now()
	}

	b.mu.Lock()
	live := b.autoplayLive
	phase := b.phase
	b.mu.Unlock()

	// Steam Cloud / CS2 Disconnected — закрываем; при «Disconnected» — повторная очередь (steam_dialog_linux.go).
	if maybeDismissSteamDialogs(b.input, b.display, &b.lastSteamDialogDismissAt) {
		select {
		case b.rematchCh <- struct{}{}:
			log.Printf("[CS2Bot:%d] Connection/CS2 dialog dismissed — scheduling matchmaking re-queue", b.display)
		default:
		}
	}

	// Память: каждый тик бота — драйвер process_vm_readv + sigscan с первого же опроса (не ждём autoplay live).
	b.pollCS2MemoryNav()
	if live {
		switch phase {
		case PhaseAlive:
			b.mu.Lock()
			b.teleGraphSteer = false
			b.mu.Unlock()
			b.syncForwardHoldClock()
			b.mu.Lock()
			nr := b.needsNavReset
			if nr {
				b.needsNavReset = false
			}
			b.mu.Unlock()
			if nr {
				b.releaseAll()
				b.pickBehavior()
			} else if time.Since(b.bhvStart) > b.bhvDuration {
				b.pickBehavior()
			}
			b.maybeUnstickForward()
			b.executeBehavior()
			b.emitNavTelemetryJSONL(time.Now())

		case PhaseDead:
			b.releaseAll()
			if b.tickCount%25 == 0 {
				b.input.Click(1)
			}
		case PhaseFreezeTime:
			b.releaseAll()
		case PhaseWaitMapLoad:
			b.tickMapReload(ctx)
		default:
			// ожидание процесса/окна/матча — движений нет, YOLO внизу всё равно крутится
		}
	}

	if b.yolo != nil {
		b.tickYoloPipeline(ctx)
	} else {
		b.tickMemEspOverlay()
	}

	b.emitMemTelemetry(true)
}

// emitMemTelemetry: один снимок на тик в desktop (WebSocket cs2:mem) — видно частоту опроса, успех чтения, ESP, nav.
func (b *CS2Bot) emitMemTelemetry(memPollRan bool) {
	fn := b.memTelemetry
	if fn == nil {
		return
	}
	hz := int(0.5 + 1.0/cs2BotTickInterval.Seconds())
	if hz < 1 {
		hz = 1
	}

	b.mu.Lock()
	memCfgPath := ResolvedCS2MemConfigPath()
	memStatus := "disabled"
	if memCfgPath != "" {
		switch {
		case b.memDriver == nil:
			memStatus = "no_driver"
		case b.lastMemPollOK:
			memStatus = "ok"
		default:
			memStatus = "error"
		}
	}
	ev := map[string]interface{}{
		"display":              b.display,
		"bot_tick":             b.tickCount,
		"ts_ms":                time.Now().UnixMilli(),
		"target_hz":            hz,
		"mem_poll_ran":         memPollRan,
		"phase":                b.phase.String(),
		"autoplay_live":        b.autoplayLive,
		"mem_driver":           b.memDriver != nil,
		"mem_poll_ok":          b.lastMemPollOK,
		"mem_poll_err":         b.lastMemPollErr,
		"mem_status":           memStatus,
		"snapshot_fail_streak": b.memSnapshotFailStreak,
		"mem_nav_active":       b.memNavActive,
		"esp_box_count":        b.lastEspBoxCount,
		"nav_route_len":        len(b.navRoute),
		"nav_route_step":       b.navRouteStep,
		"yolo":                 b.yolo != nil,
	}
	if !b.lastMemPollAt.IsZero() {
		ev["mem_poll_age_ms"] = time.Since(b.lastMemPollAt).Milliseconds()
	} else {
		ev["mem_poll_age_ms"] = nil
	}
	if b.memNavActive && !b.memAt.IsZero() {
		ev["mem_yaw_deg"] = b.memYawRad * 180 / math.Pi
		ev["mem_fresh_ms"] = time.Since(b.memAt).Milliseconds()
	}
	if b.gsiPosOK {
		ev["world_x"] = b.gsiMapX
		ev["world_y"] = b.gsiMapY
		ev["world_z"] = b.gsiMapZ
	}
	if b.lastGSI != nil && b.lastGSI.Map != nil {
		ev["map"] = strings.TrimSpace(b.lastGSI.Map.Name)
	}
	b.mu.Unlock()

	fn(b.display, ev)
}

const mapReloadMaxWait = 2 * time.Minute

// tickMapReload: after GSI reports a new map name, wait until the pawn is playable again (same as initial 5b), then re-pick behavior.
func (b *CS2Bot) tickMapReload(ctx context.Context) {
	b.releaseAll()
	b.mu.Lock()
	g := b.lastGSI
	since := b.mapReloadSince
	b.mu.Unlock()

	if gsiPawnControllable(g) {
		b.completeMapReload(ctx, g)
		return
	}
	if !since.IsZero() && time.Since(since) > mapReloadMaxWait {
		log.Printf("[CS2Bot:%d] Map reload: %v elapsed without controllable pawn — forcing behavior restart",
			b.display, mapReloadMaxWait)
		b.completeMapReload(ctx, g)
		return
	}
	if b.tickCount%20 == 0 {
		b.input.KeyTap(KeyReturn)
	}
}

func (b *CS2Bot) completeMapReload(ctx context.Context, g *GSIState) {
	mapName := gsiMapName(g)
	b.mu.Lock()
	b.sessionMapName = mapName
	b.resetBehaviorStateForNewMapLocked()
	if g != nil && g.Round != nil && g.Round.Phase == "freezetime" {
		b.phase = PhaseFreezeTime
	} else {
		b.phase = PhaseAlive
	}
	b.mapReloadSince = time.Time{}
	b.mu.Unlock()

	if mapName != "" {
		log.Printf("[CS2Bot:%d] Map reload complete — %q, new behavior", b.display, mapName)
	} else {
		log.Printf("[CS2Bot:%d] Map reload complete (map name unknown) — new behavior", b.display)
	}
	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	b.lastFocus = time.Now()
	b.pickBehavior()
}

// ─────────────────────── behavior selection ───────────────────────

func (b *CS2Bot) pickBehavior() {
	b.releaseAll()

	weights := []struct {
		kind   behaviorKind
		weight int
	}{
		{bhvRoam, 58},
		{bhvCombat, 22},
		{bhvReposition, 10},
		{bhvSprint, 6},
		{bhvCornerCheck, 4},
	}
	if b.yolo != nil {
		// Движение по карте сильнее; прицел и огонь даёт YOLO (DM: c,ch,t,th).
		weights = []struct {
			kind   behaviorKind
			weight int
		}{
			{bhvRoam, 68},
			{bhvPatrol, 12},
			{bhvCombat, 10},
			{bhvReposition, 6},
			{bhvSprint, 3},
			{bhvCornerCheck, 1},
		}
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
	case bhvRoam:
		b.bhvDuration = randDur(12000, 24000)
		b.initRoam()
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

	if b.tickCount <= 5 {
		log.Printf("[CS2Bot:%d] Behavior: %s (%.1fs)", b.display, bhvName(chosen), b.bhvDuration.Seconds())
	}
}

func (b *CS2Bot) executeBehavior() {
	elapsed := time.Since(b.bhvStart)
	switch b.behavior {
	case bhvPatrol:
		b.tickPatrol(elapsed)
	case bhvRoam:
		b.tickRoam(elapsed)
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

func (b *CS2Bot) initPatrol() {
	b.holdKey(KeyW)
	b.turn.yawRate = (15 + rand.Float64()*30) * randSign()
	b.turn.pitchRate = (2 + rand.Float64()*4) * randSign()
	b.turn.wm.Reset()
}

func (b *CS2Bot) tickPatrol(elapsed time.Duration) {
	b.ensureHeld(KeyW)

	dt := cs2BotTickInterval.Seconds()
	phase := elapsed.Seconds()

	radarScanIntervalPatrol := intervalRadarPatrol()
	if b.lastRadarScan.IsZero() || time.Since(b.lastRadarScan) >= radarScanIntervalPatrol {
		b.lastRadarScan = time.Now()
		rx, ry, rw, rh := radarROIGame()
		var wall, yawSug float64
		var rok bool
		rgb := b.input.GrabCS2RGBRect(rx, ry, rw, rh)
		if rgb != nil {
			wall, _, _, yawSug, rok = AnalyzeRadarMinimapRGB(rgb, rw, rh, &b.radarNav.emaWall)
		}
		if !rok {
			gpx := b.input.GrabCS2GrayRect(rx, ry, rw, rh)
			if gpx != nil {
				wall, _, _, yawSug, rok = AnalyzeRadarMinimap(gpx, rw, rh, &b.radarNav.emaWall)
			}
		}
		if rok && wall > 0.38 {
			b.turn.yawRate += yawSug * 0.14
			if wall > 0.52 {
				b.turn.yawRate += yawSug * 0.12
			}
		}
		b.updateRadarTelemetry(wall, yawSug, rok, rw, rh, time.Now())
	}

	yaw := b.turn.yawRate * (1.0 + 0.3*math.Sin(phase*0.7))
	pitch := b.turn.pitchRate * math.Sin(phase*1.2)

	b.smoothMouse(yaw*dt, pitch*dt)
}

// ─────────────────────── ROAM (random walk, DM-safe heuristic) ───────────────────────
// True minimap pathing would need CV or authored nav grids per map. We use GSI world X/Z for
// stuck detection, GSI Y velocity for stairs/ramp pitch hints (proxy for “lighter/darker height” on radar),
// plus strafe/back and escalating corner escapes.

func (b *CS2Bot) initRoam() {
	now := time.Now()
	b.navHeadingAt = now.Add(-time.Second)
	b.navStrafeEnd = time.Time{}
	b.navUnstickAt = now.Add(randDur(2500, 5500))
	b.navYawTarget = (rand.Float64()*2 - 1) * (30 + rand.Float64()*40)
	b.navYawSmooth = b.navYawTarget * 0.3
	b.navStrafeLeft = rand.Intn(2) == 0
	b.roamPitch = rand.Float64() * 6.28
	b.roamBackTicks = 0
	// Короче yaw-only: иначе долго едем только W без стрейфа и упираемся в геометрию.
	b.roamWallHugYawOnlyUntil = now.Add(randDur(350, 850))
	b.turn.wm.Reset()
	b.horizonPitchLean = b.horizonPitchLean*0.35 + (rand.Float64()*2-1)*0.85
	b.releaseMovement()
	b.holdKey(KeyW)
	b.planGraphNavRoute()
}

func (b *CS2Bot) tickRoam(_ time.Duration) {
	now := time.Now()
	dt := cs2BotTickInterval.Seconds()

	var memAge time.Duration
	var memSteerOK, memFresh bool
	b.mu.Lock()
	vv := b.gsiVertVel
	yawOnly := now.Before(b.roamWallHugYawOnlyUntil)
	if rand.Intn(4200) == 0 {
		b.roamWallHugYawOnlyUntil = time.Now().Add(randDur(450, 1100))
	}
	if b.memNavActive && !b.memAt.IsZero() {
		memAge = now.Sub(b.memAt)
	}
	memSteerOK = b.memNavActive && memAge < memNavMaxSteerAge
	memFresh = memSteerOK && memAge < memNavFreshAge && b.memAnglesOK
	b.mu.Unlock()

	b.mu.Lock()
	mapNameNav := b.sessionMapName
	b.mu.Unlock()
	graph := NavGraphForMap(mapNameNav)
	graphSteer := false
	if graph != nil {
		arrive := graph.routeArriveRadius()
		b.mu.Lock()
		px, pz, gsiOK := b.gsiMapX, b.gsiMapZ, b.gsiPosOK
		b.mu.Unlock()
		if gsiOK {
			if len(b.navRoute) == 0 {
				b.planGraphNavRoute()
			}
			if len(b.navRoute) > 0 {
				graphSteer = b.tickGraphRoamSteer(graph, px, pz, arrive, now)
			}
		}
	} else {
		b.navRoute = nil
		b.navRouteStep = 0
	}

	radarScanInterval := intervalRadarRoam()
	if b.lastRadarScan.IsZero() || time.Since(b.lastRadarScan) >= radarScanInterval {
		b.lastRadarScan = time.Now()
		rx, ry, rw, rh := radarROIGame()
		var wall, yawSug float64
		var rok bool
		rgb := b.input.GrabCS2RGBRect(rx, ry, rw, rh)
		if rgb != nil {
			wall, _, _, yawSug, rok = AnalyzeRadarMinimapRGB(rgb, rw, rh, &b.radarNav.emaWall)
		}
		if !rok {
			gpx := b.input.GrabCS2GrayRect(rx, ry, rw, rh)
			if gpx != nil {
				wall, _, _, yawSug, rok = AnalyzeRadarMinimap(gpx, rw, rh, &b.radarNav.emaWall)
			}
		}
		if rok {
			// Сильнее уважаем радар при навигации по графу: иначе целевой узел «сквозь стену» тянет в препятствие.
			if graphSteer && wall > 0.22 && yawSug != 0 {
				blend := math.Min(1.0, (wall-0.22)/0.38) * 0.92
				if memSteerOK {
					blend = math.Min(1.0, blend*1.15+0.05)
				}
				b.navYawTarget = b.navYawTarget*(1-blend) + yawSug*blend
				b.roamWallHugYawOnlyUntil = time.Time{}
			} else if wall > 0.34 {
				b.roamWallHugYawOnlyUntil = time.Time{}
				mul := 0.72
				if !graphSteer {
					mul = 0.55
				}
				b.navYawTarget += yawSug * mul
				if wall > 0.52 {
					if b.roamBackTicks < 9 {
						b.roamBackTicks = 9
					}
				}
			}
			if wall > 0.48 {
				b.radarStuckSamples++
				if b.radarStuckSamples >= 3 && time.Since(b.lastUnstickAt) >= unstuckCooldown {
					b.radarStuckSamples = 0
					esc := 1
					b.mu.Lock()
					if b.stuckEscalation > 0 {
						esc = b.stuckEscalation
					}
					b.mu.Unlock()
					b.doUnstickMotion(esc)
					b.mu.Lock()
					b.lastUnstickAt = time.Now()
					b.mu.Unlock()
				}
			} else if wall < 0.30 {
				b.radarStuckSamples = 0
			}
			// Память + радар: узкий порог — часто хватает одного прыжка у препятствия.
			if b.memNavActive && wall > 0.42 && b.memSpeed2 < 12.0 && math.Abs(vv) < 50 {
				if b.memStuckLowSpeedSince.IsZero() {
					b.memStuckLowSpeedSince = now
				}
			} else if b.memNavActive {
				b.memStuckLowSpeedSince = time.Time{}
			}
			if b.memNavActive && !b.memStuckLowSpeedSince.IsZero() && now.Sub(b.memStuckLowSpeedSince) > 400*time.Millisecond {
				if now.Sub(b.lastMemBumpJumpAt) > 750*time.Millisecond {
					b.input.KeyTap(KeySpace)
					b.lastMemBumpJumpAt = now
					b.memStuckLowSpeedSince = time.Time{}
					b.radarStuckSamples = 0
				}
			}
		}
		b.updateRadarTelemetry(wall, yawSug, rok, rw, rh, now)
	}

	b.mu.Lock()
	b.teleGraphSteer = graphSteer
	memYaw := b.memYawRad
	desBear := b.navDesiredBear
	anglesOK := b.memAnglesOK
	b.mu.Unlock()
	if memSteerOK && graphSteer && anglesOK {
		errDeg := angleDiffRad(desBear, memYaw) * 180 / math.Pi
		gain, maxC := 0.24, 22.0
		if memFresh {
			gain, maxC = 0.36, 32.0
		}
		b.navYawTarget += clampFloat(errDeg*gain, -maxC, maxC)
	}

	if now.After(b.navHeadingAt) {
		b.navHeadingAt = now.Add(randDur(350, 1200))
		if !graphSteer {
			b.navYawTarget = (rand.Float64()*2 - 1) * (40 + rand.Float64()*85)
		} else if memSteerOK {
			// С живой памятью и графом почти не шумим — иначе ломаем выверенный desired bearing.
			b.navYawTarget += (rand.Float64()*2 - 1) * (1.2 + rand.Float64()*3.5)
		} else {
			// Меньше случайного джиттера при графе — иначе «рвёт» к стене мимо коррекции радара.
			b.navYawTarget += (rand.Float64()*2 - 1) * (4 + rand.Float64()*11)
		}
	}

	const yawLerp = 0.09
	b.navYawSmooth += (b.navYawTarget - b.navYawSmooth) * yawLerp
	if !(graphSteer && memSteerOK) && math.Abs(b.navYawTarget-b.navYawSmooth) < 3 && rand.Float64() < 0.06 {
		if !graphSteer {
			b.navYawTarget = (rand.Float64()*2 - 1) * (50 + rand.Float64()*90)
		}
	}

	b.roamPitch += dt * (0.72 + rand.Float64()*0.55)
	// Лёгкое покачивание + слабая коррекция по лестницам (без сильного «вверх» — иначе уходит в небо).
	stairDeg := -math.Tanh(vv/195.0) * 5.2
	pitchWobble := (0.62+rand.Float64()*0.95)*math.Sin(b.roamPitch)*dt + stairDeg*dt
	b.horizonPitchLean += pitchWobble * 0.19
	b.horizonPitchLean *= 0.935
	pitchWobble -= b.horizonPitchLean * 0.52
	const roamPitchMax = 1.28
	if pitchWobble > roamPitchMax {
		pitchWobble = roamPitchMax
	}
	if pitchWobble < -roamPitchMax {
		pitchWobble = -roamPitchMax
	}
	b.smoothMouse(b.navYawSmooth*dt, pitchWobble)

	// Short backward / clear corner (not blocking; tick-count based)
	if b.roamBackTicks > 0 {
		b.releaseKey(KeyW)
		b.ensureHeld(KeyS)
		b.roamBackTicks--
	} else {
		b.releaseKey(KeyS)
		b.ensureHeld(KeyW)
		if rand.Intn(280) == 0 {
			b.roamBackTicks = 3 + rand.Intn(10)
		}
	}

	// Стрейф: реже старты, дольше удержание A/D — плавнее, чем короткие рывки 70–190 ms.
	if yawOnly {
		b.releaseKey(KeyA)
		b.releaseKey(KeyD)
	} else if now.Before(b.navStrafeEnd) {
		if b.navStrafeLeft {
			b.ensureHeld(KeyA)
			b.releaseKey(KeyD)
		} else {
			b.ensureHeld(KeyD)
			b.releaseKey(KeyA)
		}
	} else {
		b.releaseKey(KeyA)
		b.releaseKey(KeyD)
		if rand.Intn(320) == 0 {
			b.navStrafeEnd = now.Add(randDur(220, 520))
			b.navStrafeLeft = rand.Intn(2) == 0
		}
	}

	// “Unstick”: больший упор на yaw; стрейф-дополнения тоже длиннее для плавности
	if now.After(b.navUnstickAt) {
		b.navUnstickAt = now.Add(randDur(1400, 3200))
		if graphSteer && memSteerOK {
			// Не сбрасываем курс мышью наугад — память + граф уже задают heading.
			if rand.Float64() < 0.28 {
				b.navStrafeLeft = !b.navStrafeLeft
				b.navStrafeEnd = now.Add(randDur(200, 520))
			}
		} else if rand.Float64() < 0.62 {
			b.turn.wm.Reset()
			b.smoothMouse((rand.Float64()*2-1)*26, 5+rand.Float64()*10)
			b.navYawTarget = (rand.Float64()*2 - 1) * (55 + rand.Float64()*95)
			if rand.Float64() < 0.35 {
				b.navStrafeLeft = !b.navStrafeLeft
				b.navStrafeEnd = now.Add(randDur(200, 520))
			}
			if rand.Intn(3) == 0 {
				b.roamBackTicks = 4 + rand.Intn(8)
			}
		}
	}
}

// ─────────────────────── SPRINT ───────────────────────

func (b *CS2Bot) initSprint() {
	b.holdKey(KeyW)
	b.holdKey(KeyShiftL)
	b.turn.yawRate = (10 + rand.Float64()*20) * randSign()
	b.turn.pitchRate = 0
	b.turn.wm.Reset()
}

func (b *CS2Bot) tickSprint(elapsed time.Duration) {
	b.ensureHeld(KeyW)
	b.ensureHeld(KeyShiftL)

	dt := cs2BotTickInterval.Seconds()
	yaw := b.turn.yawRate
	b.smoothMouse(yaw*dt, 0)

	ms := elapsed.Milliseconds()
	if ms > 0 && ms%int64(1500+rand.Intn(1500)) < int64(cs2BotTickInterval.Milliseconds()) {
		b.input.KeyTap(KeySpace)
	}
}

// ─────────────────────── CORNER CHECK ───────────────────────

func (b *CS2Bot) initCornerCheck() {
	b.releaseMovement()
	b.turn.yawRate = 0
	b.turn.pitchRate = 0
	b.turn.wm.Reset()
}

func (b *CS2Bot) tickCornerCheck(elapsed time.Duration) {
	dur := b.bhvDuration.Seconds()
	t := elapsed.Seconds() / dur
	dt := cs2BotTickInterval.Seconds()

	var yawSpeed float64
	switch {
	case t < 0.3:
		yawSpeed = -70
	case t < 0.4:
		yawSpeed = 0
	case t < 0.8:
		yawSpeed = 55
	default:
		yawSpeed = -20
	}

	b.smoothMouse(yawSpeed*dt, 0)

	if t > 0.85 {
		b.ensureHeld(KeyW)
	}
}

// ─────────────────────── COMBAT ───────────────────────

func (b *CS2Bot) initCombat() {
	if rand.Intn(2) == 0 {
		b.holdKey(KeyCtrlL)
	}
	if rand.Intn(3) > 0 {
		b.holdKey(KeyW)
	}

	b.turn.yawRate = (20 + rand.Float64()*40) * randSign()
	b.turn.pitchRate = (-8 + rand.Float64()*16)
	b.turn.wm.Reset()

	b.burstTicks = 0
	b.burstCool = rand.Intn(15) + 5

	b.vision.Reset()
	b.lastVisionGrab = time.Time{}
	b.lastEnemyRGBGrab = time.Time{}
	b.combatWaveSeed = 0.2 + rand.Float64()*0.6
}

func (b *CS2Bot) tickCombat(elapsed time.Duration) {
	dt := cs2BotTickInterval.Seconds()
	phase := elapsed.Seconds()

	if b.yolo == nil {
		// RGB: тёплые тона T-моделей (без YOLO).
		enemyGrabEvery := intervalCombatEnemyRGB()
		if b.lastEnemyRGBGrab.IsZero() || time.Since(b.lastEnemyRGBGrab) >= enemyGrabEvery {
			b.lastEnemyRGBGrab = time.Now()
			ex, ey, ew, eh := enemyROIGame()
			rgb := b.input.GrabCS2RGBRect(ex, ey, ew, eh)
			if rgb != nil {
				if eyYaw, eyPitch, conf, ok := DetectTerroristWarmAim(rgb, ew, eh); ok && conf > 0.16 {
					bl := 0.48 + 0.35*math.Min(1, conf/0.55)
					b.smoothMouse(eyYaw*bl, eyPitch*bl*0.72)
				}
			}
		}
	}

	// Движение в прицеле — слабее; при YOLO почти не трогаем мышь (наводка из tickYoloPipeline).
	visionInterval := intervalCombatVision()
	visBlend := 0.12
	waveGain := 1.0
	if b.yolo != nil {
		visBlend = 0.04
		waveGain = 0.14
	}
	if b.lastVisionGrab.IsZero() || time.Since(b.lastVisionGrab) >= visionInterval {
		b.lastVisionGrab = time.Now()
		gray := b.input.GrabCS2CenterGray(visionROIW, visionROIH)
		if gray != nil {
			if vy, vp, vok := b.vision.Process(gray, visionROIW, visionROIH); vok {
				b.smoothMouse(vy*visBlend, vp*visBlend)
			}
		}
	}

	ws := b.combatWaveSeed
	w1 := 1.85 + ws*0.95
	w2 := 2.15 + (1-ws)*0.85
	phase2 := phase + 0.15*math.Sin(phase*0.31+float64(b.display)*0.17)
	wobble := 0.1 * math.Sin(float64(b.tickCount)*0.061+ws*4.2)

	yaw := b.turn.yawRate * (0.48*math.Cos(phase*w1) + 0.32*math.Sin(phase2*w2) + wobble) * 0.62 * waveGain
	pitch := b.turn.pitchRate * (0.52*math.Sin(phase*w1*0.88) + 0.28*math.Cos(phase2*w2*0.72)) * 0.28 * waveGain
	b.smoothMouse(yaw*dt, pitch*dt)

	if b.yolo != nil {
		return
	}
	if b.burstTicks > 0 {
		if !b.shooting {
			b.input.MouseDown(1)
			b.shooting = true
		}
		b.burstTicks--

		b.smoothMouse(0, -0.3*dt*50)

		if b.burstTicks == 0 {
			b.input.MouseUp(1)
			b.shooting = false
			b.burstCool = rand.Intn(20) + 8
		}
	} else if b.burstCool > 0 {
		b.burstCool--
	} else {
		b.burstTicks = rand.Intn(9) + 4
	}
}

// tickMemEspOverlay: рамки по памяти (W2S + entity list) при отсутствии YOLO; координаты в пикселях клиента CS2 (как у GrabCS2 / ov_root_xy).
func (b *CS2Bot) tickMemEspOverlay() {
	if b.enemyOverlay == nil {
		return
	}
	vpW, vpH := gameClientW, gameClientH
	if b.input != nil {
		if w, h, ok := b.input.CS2ClientPixelSize(); ok {
			vpW, vpH = w, h
		}
	}
	b.mu.Lock()
	drv := b.memDriver
	b.mu.Unlock()
	if drv == nil {
		b.mu.Lock()
		b.lastEspBoxCount = 0
		b.mu.Unlock()
		b.enemyOverlay.PushYolo(0, 0, vpW, vpH, nil)
		return
	}
	dets, _ := drv.espDets(vpW, vpH)
	b.mu.Lock()
	b.lastEspBoxCount = len(dets)
	b.mu.Unlock()
	b.enemyOverlay.PushYolo(0, 0, vpW, vpH, dets)
}

// tickYoloPipeline: каждый тик — захват X11 → worker → лог/превью → наводка/огонь (без искусственного throttle).
func (b *CS2Bot) tickYoloPipeline(ctx context.Context) {
	const (
		aimAlpha      = 0.10
		errAlpha      = 0.20
		shootErrTol   = 22.0
		shootConfMin  = 0.34
		burstCooldown = 14
	)

	if ctx.Err() != nil {
		return
	}

	x0, y0, rw, rh := YoloROIGame640()
	rgb := b.input.GrabCS2RGBRect(x0, y0, rw, rh)
	if rgb == nil {
		if ctx.Err() == nil && b.yoloInferTrace != nil {
			b.yoloInferTrace(b.display, 0, 0, -1, errors.New("GrabCS2RGBRect nil"))
		}
		return
	}
	awr, ahr, rok := NormalizeGrabbedRGB(rgb, rw, rh)
	if !rok {
		if ctx.Err() == nil && b.yoloInferTrace != nil {
			b.yoloInferTrace(b.display, rw, rh, -1, fmt.Errorf("RGB %d байт не сходится с ROI %dx%d (X11 подрезал кадр нестандартно)", len(rgb), rw, rh))
		}
		return
	}
	rw, rh = awr, ahr

	dets, viz, err := b.yolo.Infer(b.display, rgb, rw, rh)
	if b.yoloInferTrace != nil {
		b.yoloInferTrace(b.display, rw, rh, len(dets), err)
	}
	if err == nil && b.tickCount%384 == 0 {
		log.Printf("[CS2Bot:%d] YOLO frame dets=%d viz=%d roi=%dx%d @(%d,%d) — viz → оверлей", b.display, len(dets), len(viz), rw, rh, x0, y0)
	}
	if b.yoloPreview != nil && err == nil {
		b.yoloPreview.Push(rgb, rw, rh, viz)
	}
	if b.enemyOverlay != nil {
		if err == nil {
			b.enemyOverlay.PushYolo(x0, y0, rw, rh, viz)
		} else {
			b.enemyOverlay.PushYolo(x0, y0, rw, rh, nil)
		}
	}
	if err != nil {
		if b.tickCount%125 == 0 {
			log.Printf("[CS2Bot:%d] YOLO: %v", b.display, err)
		}
		return
	}

	best, ax, ay, errPx := PickDMBestTarget(dets, rw, rh)
	if best == nil {
		b.yoloEmaYawDeg = YoloEmaBlend(b.yoloEmaYawDeg, 0, 0.16)
		b.yoloEmaPitchDeg = YoloEmaBlend(b.yoloEmaPitchDeg, 0, 0.16)
		b.yoloSmErrPx = SmoothErrPxAlpha(b.yoloSmErrPx, 880, errAlpha)
		b.yoloStopYoloFire()
		return
	}
	b.yoloSmErrPx = SmoothErrPxAlpha(b.yoloSmErrPx, errPx, errAlpha)

	cx := float64(rw) * 0.5
	cy := float64(rh) * 0.5
	yawDeg := (ax - cx) / cx * 36
	pitchDeg := (ay - cy) / cy * 24
	b.yoloEmaYawDeg = YoloEmaBlend(b.yoloEmaYawDeg, yawDeg, aimAlpha)
	b.yoloEmaPitchDeg = YoloEmaBlend(b.yoloEmaPitchDeg, pitchDeg, aimAlpha)

	b.smoothMouse(b.yoloEmaYawDeg*0.30, b.yoloEmaPitchDeg*0.28)

	dt := cs2BotTickInterval.Seconds()
	want := b.yoloSmErrPx < shootErrTol && best.Conf >= shootConfMin && b.yoloBurstCooldown <= 0
	if want && b.yoloShotHold <= 0 {
		b.yoloShotHold = 3 + rand.Intn(5)
	}
	if b.yoloShotHold > 0 {
		if !b.shooting {
			b.input.MouseDown(1)
			b.shooting = true
		}
		b.yoloShotHold--
		b.smoothMouse(0, -0.11*dt*50)
		if b.yoloShotHold == 0 {
			b.input.MouseUp(1)
			b.shooting = false
			b.yoloBurstCooldown = burstCooldown + rand.Intn(10)
		}
	} else {
		b.yoloStopYoloFire()
	}
	if b.yoloBurstCooldown > 0 {
		b.yoloBurstCooldown--
	}
}

func (b *CS2Bot) yoloStopYoloFire() {
	if b.shooting {
		b.input.MouseUp(1)
		b.shooting = false
	}
}

// ─────────────────────── REPOSITION ───────────────────────

func (b *CS2Bot) initReposition() {
	dir := rand.Intn(3)
	switch dir {
	case 0:
		b.holdKey(KeyA)
		b.holdKey(KeyS)
	case 1:
		b.holdKey(KeyD)
		b.holdKey(KeyS)
	case 2:
		if rand.Intn(2) == 0 {
			b.holdKey(KeyA)
		} else {
			b.holdKey(KeyD)
		}
	}

	b.turn.yawRate = (30 + rand.Float64()*50) * randSign()
	b.turn.wm.Reset()
}

func (b *CS2Bot) tickReposition(elapsed time.Duration) {
	dt := cs2BotTickInterval.Seconds()
	b.smoothMouse(b.turn.yawRate*dt, 0)

	if elapsed > b.bhvDuration/2 {
		b.releaseKey(KeyS)
		b.releaseKey(KeyA)
		b.releaseKey(KeyD)
		b.ensureHeld(KeyW)
	}
}

// ─────────────────────── smooth mouse ───────────────────────

// aimAngleJitter град: случайная погрешность к заданному повороту; на совсем малых Δ не добавляем (YOLO/микрокоррекция).
func aimAngleJitter(dxDeg, dyDeg float64) (float64, float64) {
	mag := math.Hypot(dxDeg, dyDeg)
	if mag < 0.03 {
		return dxDeg, dyDeg
	}
	terr := 0.11 + math.Min(mag*0.068, 2.75)
	jYaw := (rand.Float64()*2 - 1) * terr
	jPitch := (rand.Float64()*2 - 1) * terr * 0.76
	return dxDeg + jYaw, dyDeg + jPitch
}

func (b *CS2Bot) smoothMouse(dxDeg, dyDeg float64) {
	dxDeg, dyDeg = aimAngleJitter(dxDeg, dyDeg)

	const degToPixel = 1.48
	tx := dxDeg * degToPixel
	ty := dyDeg * degToPixel
	mag := math.Hypot(tx, ty)
	if mag < 1e-8 {
		b.turn.wm.RecenterPadIfSettled()
		return
	}

	// Крупнее цель — разгон «с нуля» (ease-in); при LOW_CPU — меньше сегментов/шагов (экономия CPU).
	const microPad = 1.65
	low := cs2AutoplayLowCPU()
	microSteps, innerMax := 30, 28
	segLo, segSpan := 11, 13
	if low {
		microSteps, innerMax = 20, 18
		segLo, segSpan = 8, 9
	}
	if mag < microPad {
		b.turn.wm.AddTarget(tx, ty)
		b.runWindMouseSteps(microSteps, 5, 0.11)
		b.turn.wm.RecenterPadIfSettled()
		return
	}

	seg := segLo + rand.Intn(segSpan)
	pow := 1.14 + rand.Float64()*1.34
	var prevEase float64

	for s := 1; s <= seg; s++ {
		t := float64(s) / float64(seg)
		ease := math.Pow(t, pow)
		f := ease - prevEase
		prevEase = ease
		b.turn.wm.AddTarget(tx*f, ty*f)
		inner := 5 + (s*22)/seg + rand.Intn(4)
		if inner > innerMax {
			inner = innerMax
		}
		minK := 4
		if s <= 2 {
			minK = 6
		}
		b.runWindMouseSteps(inner, minK, 0.10)
	}
	b.turn.wm.RecenterPadIfSettled()
}

// runWindMouseSteps крутит WindMouse до достижения почти нуля дистанции или лимита шагов.
func (b *CS2Bot) runWindMouseSteps(maxSteps, minBeforeEarly int, settleEps float64) {
	for k := 0; k < maxSteps; k++ {
		mdx, mdy := b.turn.wm.Step()
		if mdx != 0 || mdy != 0 {
			b.input.MouseMove(mdx, mdy)
		}
		d := b.turn.wm.distToDest()
		if d < settleEps && k >= minBeforeEarly {
			break
		}
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

func (b *CS2Bot) ensureFocus(ctx context.Context) {
	_ = maybeDismissSteamDialogs(b.input, b.display, &b.lastSteamDialogDismissAt)
	log.Printf("[CS2Bot:%d] Ensuring game focus...", b.display)

	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	sleepCtx(ctx, 1500*time.Millisecond)
	// X11 may reconnect mid-focus; refresh cache and focus again.
	b.input.InvalidateWindowCache()
	b.input.FocusGame()
	sleepCtx(ctx, 500*time.Millisecond)

	b.lastFocus = time.Now()
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

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// Suppresses "imported and not used" for fmt
var _ = fmt.Sprintf
