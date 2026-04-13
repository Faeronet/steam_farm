//go:build linux

package autoplay

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Привязка активного драйвера (обновляется из pollCS2MemImpl) ---

var (
	memDebugMu      sync.RWMutex
	memDebugDriver  *linuxCS2Mem
	memDebugDisplay int
)

func memDebugDisplayFilter() int {
	s := strings.TrimSpace(os.Getenv(envCS2MemDebugDisplay))
	if s == "" {
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func memDebugBindDriver(display int, m *linuxCS2Mem) {
	if m == nil {
		return
	}
	want := memDebugDisplayFilter()
	if want >= 0 && display != want {
		return
	}
	memDebugMu.Lock()
	memDebugDriver = m
	memDebugDisplay = display
	memDebugMu.Unlock()
}

func memDebugTakeDriver() *linuxCS2Mem {
	memDebugMu.RLock()
	defer memDebugMu.RUnlock()
	return memDebugDriver
}

// StartCS2MemDebugHTTPServerIfConfigured слушает только 127.0.0.1. Без записи в файлы.
func StartCS2MemDebugHTTPServerIfConfigured() {
	raw := strings.TrimSpace(os.Getenv(envCS2MemDebugHTTP))
	if raw == "" || raw == "0" || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "false") || strings.EqualFold(raw, "no") {
		return
	}
	addr := raw
	if raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes") {
		addr = "127.0.0.1:17355"
	} else if strings.HasPrefix(raw, ":") {
		addr = "127.0.0.1" + raw
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("[CS2Mem:debug-http] bad %s=%q: %v", envCS2MemDebugHTTP, raw, err)
		return
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	addr = net.JoinHostPort(host, port)

	token := strings.TrimSpace(os.Getenv(envCS2MemDebugToken))
	iv := cs2MemDebugPollInterval()

	mux := http.NewServeMux()
	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !memDebugCheckToken(w, r, token) {
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		b, err := memDebugBuildJSON()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/rva-probe", func(w http.ResponseWriter, r *http.Request) {
		memDebugHandleRVAProbe(w, r, token)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		if !memDebugCheckToken(w, r, token) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", http.StatusInternalServerError)
			return
		}
		t := time.NewTicker(iv)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				b, err := memDebugBuildJSON()
				if err != nil {
					fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
				} else {
					fmt.Fprintf(w, "data: %s\n\n", b)
				}
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "CS2 mem debug (libclient via process_vm_readv)\n"+
			"  GET /snapshot   — один JSON\n"+
			"  GET /stream     — SSE, интервал %v (%s)\n", iv, envCS2MemDebugMS)
		if memMatchGateEnabled() {
			fmt.Fprintf(w, "  (по умолчанию тяжёлые snapshot/stream/rva-probe только во время матча после спавна; ?force=1 на /rva-probe; %s=0 — всегда)\n", envCS2MemMatchGate)
		}
		fmt.Fprintf(w, "  GET /rva-probe  — чтение qword по таблице so/rva_table.json (?limit=500&offset=0&block=.data)\n")
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 4 * time.Second,
	}
	auth := "off"
	if token != "" {
		auth = "on"
	}
	log.Printf("[CS2Mem:debug-http] %s (SSE /stream, JSON /snapshot); auth: %s", addr, auth)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[CS2Mem:debug-http] %v", err)
		}
	}()
}

func memDebugCheckToken(w http.ResponseWriter, r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	q := r.URL.Query().Get("token")
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[7:])
	}
	if got == token || q == token {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func memDebugOffsetsHex(o cs2MemoryJSON) map[string]string {
	out := make(map[string]string)
	add := func(k string, v uint64) {
		if v != 0 {
			out[k] = fmt.Sprintf("0x%x", v)
		}
	}
	add("dw_local_player_pawn", o.DwLocalPlayerPawn)
	add("dw_entity_list", o.DwEntityList)
	add("dw_local_player_controller", o.DwLocalPlayerController)
	add("m_h_player_pawn", o.MHPlayerPawn)
	add("m_v_old_origin", o.MvOldOrigin)
	add("m_ang_eye_angles", o.MangEyeAngles)
	add("m_vec_abs_velocity", o.MvecAbsVelocity)
	add("dw_view_matrix", o.DwViewMatrix)
	add("dw_game_entity_system", o.DwGameEntitySystem)
	add("dw_game_entity_system_highest_index", o.DwGameEntitySystemHighestIndex)
	add("m_i_team_num", o.MITeamNum)
	add("m_i_health", o.MIHealth)
	add("m_life_state", o.MLifeState)
	if o.EntityListStride != 0 {
		add("entity_list_stride", o.EntityListStride)
	}
	if o.DataSectionStartRVA != 0 {
		add("data_section_start_rva", o.DataSectionStartRVA)
	}
	if o.DataSectionPlus20hRVA != 0 {
		add("data_section_plus_20h_rva", o.DataSectionPlus20hRVA)
	}
	return out
}

type memDebugEntitySample struct {
	Index   int    `json:"index"`
	PawnHex string `json:"pawn_ptr,omitempty"`
	Team    uint32 `json:"team,omitempty"`
	Health  int32  `json:"health,omitempty"`
	Life    uint32 `json:"life_state,omitempty"`
	Err     string `json:"err,omitempty"`
}

type memDebugPayload struct {
	OK             bool            `json:"ok"`
	Err            string          `json:"err,omitempty"`
	TsMs           int64           `json:"ts_ms"`
	IntervalMs     float64         `json:"interval_ms"`
	Display        int             `json:"display"`
	PID            int             `json:"pid"`
	ClientBase     string          `json:"client_base"`
	ModulePath     string          `json:"module_path"`
	Source         string          `json:"offsets_source"`
	SigScanUsed    bool            `json:"sigscan_used"`
	MatchCollect   bool            `json:"match_collect_active"`
	MatchCollectNote string        `json:"match_collect_note,omitempty"`
	OffsetsHex     map[string]string `json:"offsets_rva_hex"`
	EspStrideHex   string          `json:"entity_list_stride_hex,omitempty"`
	EspHeight      float64         `json:"esp_player_height,omitempty"`
	EspEyeZ        float64         `json:"esp_eye_z_offset,omitempty"`
	Diag           memDiagRecord   `json:"raw_pointers"`
	Snapshot       cs2MemSnapshot  `json:"game_snapshot"`
	SnapshotErr    string          `json:"snapshot_err,omitempty"`
	ViewMatrix     []float32       `json:"view_matrix,omitempty"`
	ViewMatrixErr  string          `json:"view_matrix_err,omitempty"`
	GESPtr         string          `json:"game_entity_system_ptr,omitempty"`
	GESHighest     *uint32         `json:"game_entity_system_highest_index,omitempty"`
	GESErr         string          `json:"game_entity_system_err,omitempty"`
	EntityListPtr  string          `json:"entity_list_resolved_ptr,omitempty"`
	LocalPawnPtr   string          `json:"local_pawn_ptr,omitempty"`
	ControllerPtr  string          `json:"local_controller_ptr,omitempty"`
	EntitySample   []memDebugEntitySample `json:"entity_sample"`
}

func memDebugBuildJSON() ([]byte, error) {
	m := memDebugTakeDriver()
	if m == nil {
		b, err := json.Marshal(memDebugPayload{
			OK:         false,
			Err:        "no active mem driver (game/bot not polling yet?)",
			TsMs:       time.Now().UnixMilli(),
			IntervalMs: cs2MemDebugPollInterval().Seconds() * 1000,
		})
		return b, err
	}

	if memMatchGateEnabled() && !memDataCollectionActive(m.display) {
		off := m.off
		b, err := json.Marshal(memDebugPayload{
			OK:               true,
			TsMs:             time.Now().UnixMilli(),
			IntervalMs:       cs2MemDebugPollInterval().Seconds() * 1000,
			Display:          m.display,
			PID:              m.pid,
			ClientBase:       fmt.Sprintf("0x%x", m.clientBase),
			ModulePath:       m.selPath,
			Source:           m.sourceLabel,
			OffsetsHex:       memDebugOffsetsHex(off),
			MatchCollect:     false,
			MatchCollectNote: "paused until first controllable pawn after team/load; stops at match end (GSI). SFARM_CS2_MEM_MATCH_GATE=0 disables.",
		})
		return b, err
	}

	off := m.off
	base := m.clientBase
	pid := m.pid

	p := memDebugPayload{
		OK:             true,
		TsMs:           time.Now().UnixMilli(),
		IntervalMs:     cs2MemDebugPollInterval().Seconds() * 1000,
		Display:        m.display,
		PID:            pid,
		ClientBase:     fmt.Sprintf("0x%x", base),
		ModulePath:     m.selPath,
		Source:         m.sourceLabel,
		SigScanUsed:    strings.Contains(m.sourceLabel, "sigscan"),
		MatchCollect:   true,
		OffsetsHex:     memDebugOffsetsHex(off),
		EspStrideHex:   fmt.Sprintf("0x%x", entityStride(off)),
		EspHeight:      off.EspPlayerHeight,
		EspEyeZ:        off.EspEyeZOffset,
		EntitySample:   nil,
	}

	snap, err := m.snapshot()
	if err != nil {
		p.SnapshotErr = err.Error()
	} else {
		p.Snapshot = snap
	}
	p.Diag = m.collectMemDiag(err)

	if off.DwViewMatrix != 0 {
		mat, errVM := m.readViewMatrix()
		if errVM != nil {
			p.ViewMatrixErr = errVM.Error()
		} else {
			p.ViewMatrix = mat[:]
		}
	}

	if off.DwEntityList != 0 {
		el, errE := readU64Proc(pid, base+off.DwEntityList)
		if errE != nil {
			p.EntityListPtr = errE.Error()
		} else {
			p.EntityListPtr = fmt.Sprintf("0x%x", el)
		}
	}

	if off.DwLocalPlayerController != 0 {
		c, errC := readU64Proc(pid, base+off.DwLocalPlayerController)
		if errC != nil {
			p.ControllerPtr = errC.Error()
		} else {
			p.ControllerPtr = fmt.Sprintf("0x%x", c)
		}
	}

	pawnTry, errP := m.resolveLocalPawn()
	if errP != nil {
		p.LocalPawnPtr = errP.Error()
	} else {
		p.LocalPawnPtr = fmt.Sprintf("0x%x", pawnTry)
	}

	if off.DwGameEntitySystem != 0 && off.DwGameEntitySystemHighestIndex != 0 {
		ges, errG := readU64Proc(pid, base+off.DwGameEntitySystem)
		if errG != nil {
			p.GESErr = errG.Error()
		} else if !ptrOK(ges) {
			p.GESPtr = fmt.Sprintf("0x%x", ges)
			p.GESErr = "invalid ptr"
		} else {
			p.GESPtr = fmt.Sprintf("0x%x", ges)
			hi, errH := readU32Proc(pid, ges+off.DwGameEntitySystemHighestIndex)
			if errH != nil {
				p.GESErr = errH.Error()
			} else {
				h := hi
				p.GESHighest = &h
			}
		}
	}

	const maxSample = 32
	maxI := 128
	if off.DwGameEntitySystem != 0 && off.DwGameEntitySystemHighestIndex != 0 && p.GESHighest != nil {
		if int(*p.GESHighest) > 0 && int(*p.GESHighest) < 2048 {
			maxI = int(*p.GESHighest)
		}
	}
	if maxI > 512 {
		maxI = 512
	}

	p.EntitySample = make([]memDebugEntitySample, 0, maxSample)
	if off.DwEntityList != 0 {
		entityList, errL := readU64Proc(pid, base+off.DwEntityList)
		if errL == nil && ptrOK(entityList) {
			var localPawnU uint64
			if errP == nil {
				localPawnU = pawnTry
			}
			for i := 1; i <= maxI && len(p.EntitySample) < maxSample; i++ {
				ent, errE := m.entityPawnAtIndex(entityList, i)
				s := memDebugEntitySample{Index: i}
				if errE != nil {
					s.Err = memPollErrShort(errE.Error())
					p.EntitySample = append(p.EntitySample, s)
					continue
				}
				if ent == 0 {
					p.EntitySample = append(p.EntitySample, s)
					continue
				}
				s.PawnHex = fmt.Sprintf("0x%x", ent)
				if ent == localPawnU {
					s.Err = "local_player"
					p.EntitySample = append(p.EntitySample, s)
					continue
				}
				if off.MITeamNum != 0 {
					team, _ := readU32Proc(pid, ent+off.MITeamNum)
					s.Team = team
				}
				if off.MIHealth != 0 {
					hu, _ := readU32Proc(pid, ent+off.MIHealth)
					s.Health = int32(hu)
				}
				if off.MLifeState != 0 {
					lf, _ := readU32Proc(pid, ent+off.MLifeState)
					s.Life = lf
				}
				p.EntitySample = append(p.EntitySample, s)
			}
		}
	}

	memDebugSanitizePayload(&p)
	return json.Marshal(p)
}

func jsonSafeF64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func jsonSafeF32(v float32) float32 {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return v
}

// encoding/json не сериализует NaN/±Inf — при кривых RVA матрица/поза могут дать NaN.
func memDebugSanitizePayload(p *memDebugPayload) {
	if p == nil {
		return
	}
	s := &p.Snapshot
	s.X = jsonSafeF64(s.X)
	s.Y = jsonSafeF64(s.Y)
	s.Z = jsonSafeF64(s.Z)
	s.Pitch = jsonSafeF64(s.Pitch)
	s.Yaw = jsonSafeF64(s.Yaw)
	s.VelX = jsonSafeF64(s.VelX)
	s.VelY = jsonSafeF64(s.VelY)
	s.VelZ = jsonSafeF64(s.VelZ)
	p.EspHeight = jsonSafeF64(p.EspHeight)
	p.EspEyeZ = jsonSafeF64(p.EspEyeZ)
	for i := range p.ViewMatrix {
		p.ViewMatrix[i] = jsonSafeF32(p.ViewMatrix[i])
	}
}
