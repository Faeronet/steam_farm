package autoplay

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SFARM_CS2_NAV_LOG — путь к файлу JSONL (одна строка = один JSON за тик телеметрии).
// SFARM_CS2_NAV_LOG_MS — интервал между записями, мс (по умолчанию 150).

const envNavLog = "SFARM_CS2_NAV_LOG"
const envNavLogMs = "SFARM_CS2_NAV_LOG_MS"

func navLogInterval() time.Duration {
	ms := 150
	if s := strings.TrimSpace(os.Getenv(envNavLogMs)); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 40 && n <= 10000 {
			ms = n
		}
	}
	return time.Duration(ms) * time.Millisecond
}

func (b *CS2Bot) updateRadarTelemetry(wall, yawSug float64, ok bool, rw, rh int, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.teleRadarWall = wall
		b.teleRadarYawSug = yawSug
		b.teleRadarOK = true
		b.teleRadarAt = now
		if rw > 0 && rh > 0 {
			b.teleRadarRw = rw
			b.teleRadarRh = rh
		}
		return
	}
	b.teleRadarOK = false
}

func (b *CS2Bot) closeNavLogLocked() {
	if b.navLogF != nil {
		_ = b.navLogF.Close()
		b.navLogF = nil
		b.navLogPathOpen = ""
	}
}

func (b *CS2Bot) ensureNavLogFileLocked(path string) error {
	if b.navLogF != nil && b.navLogPathOpen == path {
		return nil
	}
	b.closeNavLogLocked()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	b.navLogF = f
	b.navLogPathOpen = path
	return nil
}

func radarHint(wall float64, ok bool) string {
	if !ok {
		return "no_minimap_frame"
	}
	switch {
	case wall < 0.22:
		return "open"
	case wall < 0.34:
		return "light_obstacle"
	case wall < 0.48:
		return "wall_near"
	default:
		return "wall_heavy_unstuck_risk"
	}
}

// navTelemetryRecord — одна запись для анализа навигации / памяти / миникарты.
type navTelemetryRecord struct {
	TS           string           `json:"ts"`
	Tick         uint64           `json:"tick"`
	Display      int              `json:"display"`
	Map          string           `json:"map"`
	Phase        string           `json:"phase"`
	Behavior     string           `json:"behavior"`
	MemConfig    string           `json:"mem_config_path,omitempty"`
	Memory       navMemTele       `json:"memory"`
	GSI          navGSITele       `json:"gsi_world"`
	Navigation   navNavTele       `json:"navigation"`
	MinimapRadar navMinimapTele   `json:"minimap_radar"`
	Movement     navMoveTele      `json:"movement"`
}

type navMemTele struct {
	NavActive  bool `json:"nav_active"`
	DriverOpen bool `json:"driver_open,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	YawDeg    float64 `json:"yaw_deg,omitempty"`
	SpeedXZ   float64 `json:"speed_xz,omitempty"`
	VertVel   float64 `json:"vert_vel_ema,omitempty"`
	AnglesOK  bool    `json:"angles_ok"`
	AgeMs     int64   `json:"sample_age_ms,omitempty"`
}

type navGSITele struct {
	OK       bool    `json:"ok"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Z        float64 `json:"z,omitempty"`
	FrozenMs int64   `json:"frozen_ms,omitempty"`
}

type navNavTele struct {
	GraphPresent   bool    `json:"graph_present"`
	GraphSteer     bool    `json:"graph_steer"`
	RouteLen       int     `json:"route_len"`
	RouteStep      int     `json:"route_step"`
	TargetNodeIdx  int     `json:"target_node_idx,omitempty"`
	TargetX        float64 `json:"target_x,omitempty"`
	TargetZ        float64 `json:"target_z,omitempty"`
	TargetLabel    string  `json:"target_label,omitempty"`
	NavYawTarget   float64 `json:"nav_yaw_target_deg_per_s"`
	NavYawSmooth   float64 `json:"nav_yaw_smooth_deg_per_s"`
	DesiredBearDeg float64 `json:"desired_bearing_deg,omitempty"`
	MoveBearDeg    float64 `json:"move_bearing_deg,omitempty"`
	StuckEsc       int     `json:"stuck_escalation"`
	RadarStuckN    int     `json:"radar_stuck_samples"`
}

type navMinimapTele struct {
	OK               bool    `json:"analysis_ok"`
	AgeMs            int64   `json:"sample_age_ms"`
	Roi              string  `json:"roi_wh,omitempty"`
	WallScore        float64 `json:"wall_score"`
	YawSuggestionDeg float64 `json:"yaw_suggestion_mouse_units"`
	Hint             string  `json:"hint"`
}

type navMoveTele struct {
	KeyW    bool `json:"w"`
	KeyA    bool `json:"a"`
	KeyS    bool `json:"s"`
	KeyD    bool `json:"d"`
	Shift   bool `json:"shift"`
	Ctrl    bool `json:"ctrl"`
	BackpedalTicks int `json:"roam_back_ticks,omitempty"`
}

var navLogStartOnce sync.Map // display -> logged

func (b *CS2Bot) emitNavTelemetryJSONL(now time.Time) {
	pathEnv := strings.TrimSpace(os.Getenv(envNavLog))
	if strings.EqualFold(pathEnv, "-") || strings.EqualFold(pathEnv, "0") || strings.EqualFold(pathEnv, "off") {
		return
	}
	path := pathEnv
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("sfarm_cs2_nav_disp%d.jsonl", b.display))
	}
	gap := navLogInterval()

	b.mu.Lock()
	if !b.navLogLastEmit.IsZero() && now.Sub(b.navLogLastEmit) < gap {
		b.mu.Unlock()
		return
	}
	b.navLogLastEmit = now

	if err := b.ensureNavLogFileLocked(path); err != nil {
		b.mu.Unlock()
		return
	}
	if _, loaded := navLogStartOnce.LoadOrStore(b.display, true); !loaded {
		log.Printf("[CS2Bot:%d] nav telemetry JSONL → %q (interval %v, SFARM_CS2_NAV_LOG_MS)", b.display, path, gap)
	}

	rec := navTelemetryRecord{
		TS:       now.UTC().Format(time.RFC3339Nano),
		Tick:     b.tickCount,
		Display:  b.display,
		Map:      b.sessionMapName,
		Phase:    b.phase.String(),
		Behavior: bhvName(b.behavior),
	}
	if p := ResolvedCS2MemConfigPath(); p != "" {
		rec.MemConfig = p
	}

	rec.Memory.NavActive = b.memNavActive
	rec.Memory.DriverOpen = b.memDriver != nil
	rec.Memory.AnglesOK = b.memAnglesOK
	if b.memNavActive {
		rec.Memory.X, rec.Memory.Y, rec.Memory.Z = b.gsiMapX, b.gsiMapY, b.gsiMapZ
		rec.Memory.YawDeg = b.memYawRad * 180 / 3.141592653589793
		if !b.memAt.IsZero() {
			rec.Memory.AgeMs = now.Sub(b.memAt).Milliseconds()
		}
	}
	rec.Memory.VertVel = b.gsiVertVel
	if b.memNavActive && b.memSpeed2 > 0 {
		rec.Memory.SpeedXZ = math.Sqrt(b.memSpeed2)
	}

	rec.GSI.OK = b.gsiPosOK
	rec.GSI.X, rec.GSI.Y, rec.GSI.Z = b.gsiMapX, b.gsiMapY, b.gsiMapZ
	if !b.gsiFrozenSince.IsZero() {
		rec.GSI.FrozenMs = now.Sub(b.gsiFrozenSince).Milliseconds()
	}

	g := NavGraphForMap(b.sessionMapName)
	rec.Navigation.GraphPresent = g != nil
	route := append([]int(nil), b.navRoute...)
	step := b.navRouteStep
	rec.Navigation.RouteLen = len(route)
	rec.Navigation.RouteStep = step
	rec.Navigation.NavYawTarget = b.navYawTarget
	rec.Navigation.NavYawSmooth = b.navYawSmooth
	rec.Navigation.DesiredBearDeg = b.navDesiredBear * 180 / 3.141592653589793
	rec.Navigation.MoveBearDeg = b.navMoveBear * 180 / 3.141592653589793
	rec.Navigation.StuckEsc = b.stuckEscalation
	rec.Navigation.RadarStuckN = b.radarStuckSamples

	if g != nil && len(route) > 0 && step < len(route) {
		idx := route[step]
		if idx >= 0 && idx < len(g.nodes) {
			n := g.nodes[idx]
			rec.Navigation.TargetNodeIdx = idx
			rec.Navigation.TargetX = n.X
			rec.Navigation.TargetZ = n.Z
			rec.Navigation.TargetLabel = n.Label
		}
	}
	rec.Navigation.GraphSteer = b.teleGraphSteer

	rec.MinimapRadar.WallScore = b.teleRadarWall
	rec.MinimapRadar.YawSuggestionDeg = b.teleRadarYawSug
	rec.MinimapRadar.OK = b.teleRadarOK
	rec.MinimapRadar.Hint = radarHint(b.teleRadarWall, b.teleRadarOK)
	if b.teleRadarRw > 0 {
		rec.MinimapRadar.Roi = strconv.Itoa(b.teleRadarRw) + "x" + strconv.Itoa(b.teleRadarRh)
	}
	if b.teleRadarOK && !b.teleRadarAt.IsZero() {
		rec.MinimapRadar.AgeMs = now.Sub(b.teleRadarAt).Milliseconds()
	}

	if b.heldKeys != nil {
		rec.Movement.KeyW = b.heldKeys[KeyW]
		rec.Movement.KeyA = b.heldKeys[KeyA]
		rec.Movement.KeyS = b.heldKeys[KeyS]
		rec.Movement.KeyD = b.heldKeys[KeyD]
		rec.Movement.Shift = b.heldKeys[KeyShiftL]
		rec.Movement.Ctrl = b.heldKeys[KeyCtrlL]
	}
	rec.Movement.BackpedalTicks = b.roamBackTicks

	buf, err := json.Marshal(rec)
	f := b.navLogF
	b.mu.Unlock()
	if err != nil || f == nil {
		return
	}
	buf = append(buf, '\n')
	_, _ = f.Write(buf)
}
