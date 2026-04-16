//go:build linux

package autoplay

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const memSnapshotFailsBeforeDriverDrop = 40 // ~0.6s @ 64 Hz — не ронять memDriver после одного сбоя, иначе ESP не успевает читать.

var (
	memW2SColMajor bool
	espSkipFwdDot  bool
	espFwdDotMin   = 0.34
)

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_MATRIX_LAYOUT"))) {
	case "col", "column", "colmajor":
		memW2SColMajor = true
	}
	if v := strings.TrimSpace(os.Getenv("SFARM_CS2_ESP_SKIP_FWD_FILTER")); v == "1" || strings.EqualFold(v, "true") {
		espSkipFwdDot = true
	}
	if v := strings.TrimSpace(os.Getenv("SFARM_CS2_ESP_FWD_DOT")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= -1.01 && f <= 1.01 {
			espFwdDotMin = f
		}
	}
}

var espMissingWarnMu sync.Mutex
var espMissingWarnAt = make(map[int]time.Time)

func logEspMissingOffsets(display int, keys []string) {
	if len(keys) == 0 {
		return
	}
	now := time.Now()
	espMissingWarnMu.Lock()
	defer espMissingWarnMu.Unlock()
	if t, ok := espMissingWarnAt[display]; ok && now.Sub(t) < 30*time.Second {
		return
	}
	espMissingWarnAt[display] = now
	log.Printf("[CS2Mem:%d] espDets: нет оффсетов (0) — заполните cs2_memory.json: %s", display, strings.Join(keys, ", "))
}

func memPollErrShort(msg string) string {
	if len(msg) <= 160 {
		return msg
	}
	return msg[:160] + "…"
}

// Ошибки разбора указателей (dwEntityList / pawn и т.д.) = неверные RVA для Linux, а не «мертвый» PID.
// Сбрасывать memDriver из‑за них бессмысленно и даёт мигание «no driver» в UI ~каждые 0.6s.
func memSnapshotErrCountsTowardDriverDrop(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	memLayoutNoise :=
		strings.Contains(s, "invalid pawn ptr") ||
			strings.Contains(s, "invalid entity list ptr") ||
			strings.Contains(s, "invalid controller ptr") ||
			strings.Contains(s, "invalid list chunk ptr") ||
			strings.Contains(s, "invalid resolved pawn ptr") ||
			strings.Contains(s, "invalid entity handle")
	if memLayoutNoise {
		// I/O или процесс исчез — драйвер всё же переподнимаем.
		if strings.Contains(s, "process_vm_readv") ||
			strings.Contains(s, "short read") ||
			strings.Contains(s, "eperm") ||
			strings.Contains(s, "eacces") ||
			strings.Contains(s, "no such process") ||
			strings.Contains(s, "esrch") {
			return true
		}
		return false
	}
	return true
}

// memDiag* — SFARM_CS2_MEM_DIAG: сырые слова по текущим RVA (для подбора Linux-офсетов, не «дамп всей RAM»).
var memDiagMu sync.Mutex
var memDiagLastEmit = make(map[int]time.Time)

type memDiagRecord struct {
	TsMs                int64  `json:"ts_ms"`
	Display             int    `json:"display"`
	PID                 int    `json:"pid"`
	ClientBase          string `json:"client_base"`
	ModulePath          string `json:"module_path"`
	Source              string `json:"source"`
	DwLocalPlayerPawn   string `json:"dw_local_player_pawn"`
	DwEntityList        string `json:"dw_entity_list"`
	DwController        string `json:"dw_local_player_controller"`
	DwViewMatrix        string `json:"dw_view_matrix"`
	RawAtLPP            string `json:"raw_u64_at_dw_local_player_pawn"`
	RawAtEnt            string `json:"raw_u64_at_dw_entity_list"`
	RawAtCtrl           string `json:"raw_u64_at_dw_local_player_controller"`
	LocalPawnPtrOK      *bool  `json:"local_pawn_ptr_ok,omitempty"`
	EntityListPtrOK     *bool  `json:"entity_list_ptr_ok,omitempty"`
	ControllerPtrOK     *bool  `json:"controller_ptr_ok,omitempty"`
	RawMHPlayerPawn     string `json:"raw_u32_m_h_player_pawn,omitempty"`
	ViewMatrixFirst32Hex string `json:"view_matrix_first_32b_hex,omitempty"`
	SnapshotErr         string `json:"snapshot_err,omitempty"`
}

func (m *linuxCS2Mem) collectMemDiag(snapshotErr error) memDiagRecord {
	rec := memDiagRecord{
		TsMs:               time.Now().UnixMilli(),
		Display:            m.display,
		PID:                m.pid,
		ClientBase:         fmt.Sprintf("0x%x", m.clientBase),
		ModulePath:         m.selPath,
		Source:             m.sourceLabel,
		DwLocalPlayerPawn:  fmt.Sprintf("0x%x", m.off.DwLocalPlayerPawn),
		DwEntityList:       fmt.Sprintf("0x%x", m.off.DwEntityList),
		DwController:       fmt.Sprintf("0x%x", m.off.DwLocalPlayerController),
		DwViewMatrix:       fmt.Sprintf("0x%x", m.off.DwViewMatrix),
	}
	if snapshotErr != nil {
		rec.SnapshotErr = snapshotErr.Error()
	}
	pid, base := m.pid, m.clientBase
	if off := m.off.DwLocalPlayerPawn; off != 0 {
		u, err := readU64Proc(pid, base+off)
		if err != nil {
			rec.RawAtLPP = err.Error()
		} else {
			rec.RawAtLPP = fmt.Sprintf("0x%x", u)
			ok := ptrOK(u)
			rec.LocalPawnPtrOK = &ok
		}
	}
	if off := m.off.DwEntityList; off != 0 {
		u, err := readU64Proc(pid, base+off)
		if err != nil {
			rec.RawAtEnt = err.Error()
		} else {
			rec.RawAtEnt = fmt.Sprintf("0x%x", u)
			ok := ptrOK(u)
			rec.EntityListPtrOK = &ok
		}
	}
	if off := m.off.DwLocalPlayerController; off != 0 {
		u, err := readU64Proc(pid, base+off)
		if err != nil {
			rec.RawAtCtrl = err.Error()
		} else {
			rec.RawAtCtrl = fmt.Sprintf("0x%x", u)
			ok := ptrOK(u)
			rec.ControllerPtrOK = &ok
			if ok && m.off.MHPlayerPawn != 0 {
				h, err2 := readU32Proc(pid, u+m.off.MHPlayerPawn)
				if err2 != nil {
					rec.RawMHPlayerPawn = err2.Error()
				} else {
					rec.RawMHPlayerPawn = fmt.Sprintf("0x%x", h)
				}
			}
		}
	}
	if off := m.off.DwViewMatrix; off != 0 {
		var buf [32]byte
		if err := readProcMem(pid, base+off, buf[:]); err != nil {
			rec.ViewMatrixFirst32Hex = err.Error()
		} else {
			rec.ViewMatrixFirst32Hex = hex.EncodeToString(buf[:])
		}
	}
	return rec
}

func maybeEmitLinuxMemDiag(m *linuxCS2Mem, snapshotErr error) {
	if memMatchGateEnabled() && !memDataCollectionActive(m.display) {
		return
	}
	logOut, path := cs2MemDiagTarget()
	if !logOut && path == "" {
		return
	}
	iv := cs2MemDiagInterval()
	now := time.Now()
	memDiagMu.Lock()
	if prev, ok := memDiagLastEmit[m.display]; ok && now.Sub(prev) < iv {
		memDiagMu.Unlock()
		return
	}
	memDiagLastEmit[m.display] = now
	memDiagMu.Unlock()

	rec := m.collectMemDiag(snapshotErr)
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	if path != "" {
		memDiagAppendLine(path, line)
	}
	if logOut {
		log.Printf("[CS2Mem:%d] diag %s", m.display, string(line))
	}
}

// Один открытый файл на путь — не open/close на каждый тик при SFARM_CS2_MEM_DIAG_MS ≈ 15.6
var memDiagFileMu sync.Mutex
var memDiagOpenPath string
var memDiagOpenF *os.File

func memDiagAppendLine(path string, line []byte) {
	memDiagFileMu.Lock()
	defer memDiagFileMu.Unlock()
	if memDiagOpenF != nil && memDiagOpenPath != path {
		_ = memDiagOpenF.Close()
		memDiagOpenF = nil
		memDiagOpenPath = ""
	}
	if memDiagOpenF == nil {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[CS2Mem] diag file %q: %v", path, err)
			return
		}
		memDiagOpenPath = path
		memDiagOpenF = f
	}
	_, _ = memDiagOpenF.Write(append(append([]byte(nil), line...), '\n'))
}

// Один полный дамп libclient.so (все читаемые VMA с этим путём) + proc maps; не вся VAS процесса (см. README в каталоге).
var linuxMemModuleDumpOnce sync.Map // "display|pid|pathCore" -> struct{}

func mapsPathCore(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " ("); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func maybeScheduleModuleMemoryDump(display int, m *linuxCS2Mem) {
	parent := cs2MemModuleDumpParentDir()
	if parent == "" {
		return
	}
	key := fmt.Sprintf("%d|%d|%s", display, m.pid, mapsPathCore(m.selPath))
	if _, loaded := linuxMemModuleDumpOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	mm := m
	disp := display
	go func() {
		dir := filepath.Join(parent, fmt.Sprintf("sfarm_cs2_module_disp%d_pid%d_%d", disp, mm.pid, time.Now().Unix()))
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[CS2Mem:%d] module dump: mkdir %s: %v", disp, dir, err)
			return
		}
		mapPath := filepath.Join("/proc", strconv.Itoa(mm.pid), "maps")
		raw, err := os.ReadFile(mapPath)
		if err != nil {
			log.Printf("[CS2Mem:%d] module dump: read maps: %v", disp, err)
		} else if err := os.WriteFile(filepath.Join(dir, "proc_maps_full.txt"), raw, 0644); err != nil {
			log.Printf("[CS2Mem:%d] module dump: write maps: %v", disp, err)
		}
		segDir := filepath.Join(dir, "libclient_so_segments")
		maxB := cs2MemModuleDumpMaxBytes()
		sz, err := dumpLibclientSegmentsToDir(mm.pid, mm.selPath, segDir, maxB)
		if err != nil {
			log.Printf("[CS2Mem:%d] module dump segments: %v", disp, err)
		}
		readme := fmt.Sprintf(
			"База модуля (как в драйвере): 0x%x\nПуть ELF: %s\nСегменты libclient в libclient_so_segments/: ~%.2f MiB (см. manifest.txt)\n\n"+
				"Это НЕ вся память процесса CS2 — только области maps с этим путём (образ client в RAM).\n"+
				"Полная VAS обычно несколько ГБ (куча, драйверы, anon); такой дамп бессмысленен по размеру.\n"+
				"RVA из offsets.json = относительно image base (минимальный vaddr модуля).\n",
			mm.clientBase, mm.selPath, float64(sz)/(1024*1024))
		_ = os.WriteFile(filepath.Join(dir, "README.txt"), []byte(readme), 0644)
		log.Printf("[CS2Mem:%d] module memory dump → %s (~%.1f MiB segments)", disp, dir, float64(sz)/(1024*1024))
	}()
}

func dumpLibclientSegmentsToDir(pid int, selPath, segDir string, maxTotal int64) (written int64, err error) {
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return 0, err
	}
	mapData, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "maps"))
	if err != nil {
		return 0, err
	}
	selCore := mapsPathCore(selPath)
	var manifest strings.Builder
	idx := 0
	const chunk = 512 * 1024
	trunc := false
	for _, line := range strings.Split(string(mapData), "\n") {
		st, en, perms, mpath, ok := parseMapsLine(line)
		if !ok || mpath == "" || strings.HasPrefix(mpath, "[") {
			continue
		}
		if mapsPathCore(mpath) != selCore {
			continue
		}
		if len(perms) < 1 || perms[0] != 'r' {
			continue
		}
		sz := int64(en - st)
		if sz <= 0 {
			continue
		}
		if written+sz > maxTotal {
			trunc = true
			break
		}
		_, _ = fmt.Fprintf(&manifest, "%03d vaddr=0x%x-0x%x %s size=%d path=%s\n", idx, st, en, perms, sz, mpath)
		fname := filepath.Join(segDir, fmt.Sprintf("seg_%03d_%016x-%016x_%s.bin", idx, st, en, perms))
		idx++
		f, ferr := os.Create(fname)
		if ferr != nil {
			return written, ferr
		}
		for addr := st; addr < en; {
			n := uint64(chunk)
			if addr+n > en {
				n = en - addr
			}
			buf := make([]byte, n)
			if rerr := readProcMem(pid, addr, buf); rerr != nil {
				_ = f.Close()
				return written, fmt.Errorf("segment vaddr 0x%x +0x%x: %w", addr, n, rerr)
			}
			var nw int
			nw, ferr = f.Write(buf)
			written += int64(nw)
			if ferr != nil {
				_ = f.Close()
				return written, ferr
			}
			addr += n
		}
		_ = f.Close()
	}
	if trunc {
		_, _ = manifest.WriteString("\n# truncated: SFARM_CS2_MEM_MODULE_DUMP_MAX_MB\n")
	}
	_ = os.WriteFile(filepath.Join(segDir, "manifest.txt"), []byte(manifest.String()), 0644)
	return written, nil
}

// Throttle repeated "init OK" when the driver is recreated after read errors.
var linuxMemInitLogMu sync.Mutex
var linuxMemInitLogAt = make(map[string]time.Time)

// One hint per display: a2x main branch offsets target Windows client.dll, not Linux libclient.so.
var linuxDumperOffsetsMismatchHint sync.Map // int -> struct{}

type linuxCS2Mem struct {
	pid        int
	clientBase uint64
	selPath    string
	off        cs2MemoryJSON
	sourceLabel string
	display    int
	lastErrMsg string
	lastErrLog time.Time
}

func pollCS2MemImpl(b *CS2Bot) {
	off, src, err := resolveCS2MemoryOffsets()
	if err != nil {
		b.mu.Lock()
		if !b.memNavHintLogged {
			b.memNavHintLogged = true
			log.Printf("[CS2Bot:%d] CS2 mem nav OFF: %v", b.display, err)
		}
		b.mu.Unlock()
		return
	}
	now := time.Now()

	b.mu.Lock()
	if !b.memReaderNextTry.IsZero() && now.Before(b.memReaderNextTry) {
		b.mu.Unlock()
		return
	}
	drv := b.memDriver
	display := b.display
	b.mu.Unlock()

	if drv != nil {
		emitSigScanSkipNoticeIfReady(display, off)
	}

	if drv == nil {
		// Sigscan для заполнения dw_* из .text — с первого тика (CS2 запущен, PID есть). Ворота матча
		// (SFARM_CS2_MEM_MATCH_GATE) по-прежнему только для MEM_DIAG / дампов / rva-probe, не для драйвера.
		allowSigScan := true
		d := tryStartLinuxMemDriver(display, off, src, allowSigScan)
		b.mu.Lock()
		b.memDriver = d
		if d == nil {
			b.memReaderNextTry = now.Add(2 * time.Second)
		} else {
			b.memSnapshotFailStreak = 0
		}
		b.mu.Unlock()
		if d == nil {
			return
		}
		drv = d
	}

	if m0, ok := drv.(*linuxCS2Mem); ok {
		memDebugBindDriver(display, m0)
	}
	snap, err := drv.snapshot()
	m := drv.(*linuxCS2Mem)
	maybeEmitLinuxMemDiag(m, err)

	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		b.memLastOKLog = time.Time{}
		if shouldLogMemReadErrLocked(b, err.Error(), now) {
			log.Printf("[CS2Mem:%d] read error (pid=%d base=0x%x): %v — if EPERM: same user as game, no hidepid; snap/ns sandbox may block process_vm_readv",
				display, m.pid, m.clientBase, err)
		}
		logLinuxDumperOffsetsMismatchOnce(display, m, err.Error())
		b.lastMemPollAt = now
		b.lastMemPollOK = false
		b.lastMemPollErr = memPollErrShort(err.Error())
		if memSnapshotErrCountsTowardDriverDrop(err) {
			b.memSnapshotFailStreak++
			if b.memSnapshotFailStreak >= memSnapshotFailsBeforeDriverDrop {
				b.memSnapshotFailStreak = 0
				b.memDriver = nil
				b.memReaderNextTry = now.Add(400 * time.Millisecond)
			}
		} else {
			b.memSnapshotFailStreak = 0
		}
		b.memNavActive = false
		return
	}
	b.memSnapshotFailStreak = 0
	if !snapshotWorldSane(snap) {
		b.memLastOKLog = time.Time{}
		b.lastMemPollAt = now
		b.lastMemPollOK = false
		b.lastMemPollErr = "sanity_reject_coords"
		if shouldLogMemErr(m, "sanity: garbage world coords (wrong module base or offsets?)", now) {
			log.Printf("[CS2Mem:%d] rejected snapshot pos=(%.2f, %.2f, %.2f) — check module_path_contains / dump RVAs",
				display, snap.X, snap.Y, snap.Z)
		}
		b.memNavActive = false
		return
	}

	prevX, prevZ := b.gsiMapX, b.gsiMapZ
	hadPos := b.gsiPosOK
	b.memReaderNextTry = time.Time{}
	b.memReadErrMsg = ""
	b.memReadErrLastLog = time.Time{}
	b.ingestMemWorldPositionLocked(snap)
	b.lastMemPollAt = now
	b.lastMemPollOK = true
	b.lastMemPollErr = ""

	const memOKLogEvery = 20 * time.Second
	if b.memLastOKLog.IsZero() {
		b.memLastOKLog = now
		log.Printf("[CS2Mem:%d] read OK — snapshot active, process_vm_readv pid=%d nav=%v pos=(%.0f,%.0f,%.0f) yaw=%.1f°",
			display, m.pid, b.memNavActive, snap.X, snap.Y, snap.Z, snap.Yaw)
	} else if now.Sub(b.memLastOKLog) >= memOKLogEvery {
		b.memLastOKLog = now
		log.Printf("[CS2Mem:%d] read OK pid=%d nav=%v pos=(%.0f,%.0f,%.0f)", display, m.pid, b.memNavActive, snap.X, snap.Y, snap.Z)
	}

	if cs2MemVerbose() {
		if b.memLastVerboseLog.IsZero() || now.Sub(b.memLastVerboseLog) >= 3*time.Second {
			b.memLastVerboseLog = now
			log.Printf("[CS2Mem:%d] sample pos=(%.1f, %.1f, %.1f) yaw=%.1f° vel=(%.1f,%.1f,%.1f) velOK=%v anglesOK=%v",
				display, snap.X, snap.Y, snap.Z, snap.Yaw, snap.VelX, snap.VelY, snap.VelZ, snap.VelOK, snap.AnglesOK)
		}
	}

	if hadPos && b.gsiPosOK {
		dgx := snap.X - prevX
		dgz := snap.Z - prevZ
		dist := math.Sqrt(dgx*dgx + dgz*dgz)
		if dist > 2400 && (b.lastMemGSIDriftLog.IsZero() || now.Sub(b.lastMemGSIDriftLog) > 20*time.Second) {
			b.lastMemGSIDriftLog = now
			log.Printf("[CS2Mem:%d] large step vs prior anchor Δxz=%.0f — respawn/teleport or bad mem (was %.0f,%.0f → %.0f,%.0f)",
				display, dist, prevX, prevZ, snap.X, snap.Z)
		}
		if b.lastGSI != nil && b.lastGSI.Player != nil {
			pos := strings.TrimSpace(b.lastGSI.Player.Position)
			if gx, gy, gz, ok := parseVec3FromGSI(pos); ok {
				d := math.Sqrt((snap.X-gx)*(snap.X-gx) + (snap.Y-gy)*(snap.Y-gy) + (snap.Z-gz)*(snap.Z-gz))
				if d > 180 && (b.lastMemGSIDriftLog.IsZero() || now.Sub(b.lastMemGSIDriftLog) > 22*time.Second) {
					b.lastMemGSIDriftLog = now
					log.Printf("[CS2Mem:%d] |mem-GSI|=%.0f u (mem %.0f,%.0f,%.0f vs GSI %.0f,%.0f,%.0f) — offsets or module base wrong",
						display, d, snap.X, snap.Y, snap.Z, gx, gy, gz)
				}
			}
		}
	}
}

func logLinuxDumperOffsetsMismatchOnce(display int, m *linuxCS2Mem, errMsg string) {
	if m == nil {
		return
	}
	pl := strings.ToLower(m.selPath)
	if !strings.Contains(pl, "libclient.so") || !strings.Contains(pl, "linux") {
		return
	}
	if !strings.Contains(m.sourceLabel, "cs2_dumper") {
		return
	}
	if !strings.Contains(errMsg, "invalid pawn ptr") && !strings.Contains(errMsg, "invalid entity list ptr") &&
		!strings.Contains(errMsg, "invalid controller ptr") && !strings.Contains(errMsg, "invalid list chunk ptr") {
		return
	}
	if _, loaded := linuxDumperOffsetsMismatchHint.LoadOrStore(display, true); loaded {
		return
	}
	log.Printf("[CS2Mem:%d] hint: «make cs2-offsets» = a2x/cs2-dumper **main** (RVA для Windows client.dll). Игра у вас Linux (%s) — те же RVA в .data/.bss **другие**. Нужны офсеты для **libclient.so** или config/cs2_memory.json. Опрос памяти продолжается (для респавнов в DM и т.д.); шум в логах режется.",
		display, filepath.Base(m.selPath))
}

// shouldLogMemReadErrLocked: throttle while memDriver is recreated each failed read (driver's lastErr* would reset).
func shouldLogMemReadErrLocked(b *CS2Bot, msg string, now time.Time) bool {
	if b.memReadErrMsg != msg {
		b.memReadErrMsg = msg
		b.memReadErrLastLog = time.Time{}
	}
	if b.memReadErrLastLog.IsZero() || now.Sub(b.memReadErrLastLog) >= 30*time.Second {
		b.memReadErrLastLog = now
		return true
	}
	return false
}

func shouldLogMemErr(m *linuxCS2Mem, msg string, now time.Time) bool {
	if m.lastErrMsg != msg {
		m.lastErrMsg = msg
		m.lastErrLog = time.Time{}
	}
	if m.lastErrLog.IsZero() || now.Sub(m.lastErrLog) >= 4*time.Second {
		m.lastErrLog = now
		return true
	}
	return false
}

var linuxSigScanDeferLogMu sync.Mutex
var linuxSigScanDeferLogAt = map[int]time.Time{}

var linuxMemPawnZeroLogMu sync.Mutex
var linuxMemPawnZeroLogAt = map[int]time.Time{}

func logSigScanDeferredThrottled(display int) {
	now := time.Now()
	linuxSigScanDeferLogMu.Lock()
	if t, ok := linuxSigScanDeferLogAt[display]; ok && now.Sub(t) < 20*time.Second {
		linuxSigScanDeferLogMu.Unlock()
		return
	}
	linuxSigScanDeferLogAt[display] = now
	linuxSigScanDeferLogMu.Unlock()
	msg := "sigscanner: отложен до управляемого спавна (как SFARM_CS2_MEM_MATCH_GATE) — в GSI нужны map, activity=playing, hp>0 и не freezetime; раньше: SFARM_CS2_MEM_MATCH_GATE=0"
	log.Printf("[CS2Mem:%d] %s", display, msg)
	if fn := SigScanLogFunc; fn != nil {
		fn("info", msg)
	}
}

func tryStartLinuxMemDriver(display int, off cs2MemoryJSON, sourceLabel string, allowSigScan bool) cs2MemDriver {
	// m_v_old_origin must be non-zero (struct field offset from dumper, same on Windows/Linux).
	if off.MvOldOrigin == 0 {
		log.Printf("[CS2Mem:%d] offsets from %q: m_v_old_origin must be non-zero", display, sourceLabel)
		return nil
	}
	pid, ok := cs2PIDForDisplay(display)
	if !ok || pid <= 0 {
		log.Printf("[CS2Mem:%d] no cs2 pid (DISPLAY=:%d match failed); set SFARM_CS2_PID or fix DISPLAY on game process", display, display)
		return nil
	}
	base, selPath, err := moduleImageBase(pid, off)
	if err != nil || base == 0 {
		log.Printf("[CS2Mem:%d] module base pid=%d substr=%q: %v — hint: /proc/%d/maps | grep -i client",
			display, pid, off.ModuleSubstr, err, pid)
		logGrepHintMaps(pid, off.ModuleSubstr)
		return nil
	}

	needSigFill := offsetsNeedLibclientSigScanFill(off)
	if !allowSigScan && needSigFill {
		logSigScanDeferredThrottled(display)
		return nil
	}

	// Тяжёлый .text-скан только при открытых воротах и когда в конфиге чего-то не хватает.
	var sigFound map[string]uint64
	var sigErr error
	if needSigFill && allowSigScan {
		sigFound, sigErr = sigScanLibclient(pid, base, selPath)
	}
	if sigErr != nil {
		log.Printf("[CS2Mem:%d] sigscanner: %v", display, sigErr)
	}
	nApplied := applySigScanToOffsets(&off, sigFound)
	if nApplied > 0 {
		sourceLabel = strings.Replace(sourceLabel, "sigscan-pending", "sigscan", 1)
		if !strings.Contains(sourceLabel, "sigscan") {
			sourceLabel += "+sigscan"
		}
	}

	if off.DwLocalPlayerPawn == 0 {
		now := time.Now()
		linuxMemPawnZeroLogMu.Lock()
		throttle := false
		if t, ok := linuxMemPawnZeroLogAt[display]; ok && now.Sub(t) < 25*time.Second {
			throttle = true
		} else {
			linuxMemPawnZeroLogAt[display] = now
		}
		linuxMemPawnZeroLogMu.Unlock()
		if !throttle {
			if nApplied > 0 {
				log.Printf("[CS2Mem:%d] sigscanner applied %d offsets: dwPawn=0x%x ent=0x%x ctrl=0x%x vm=0x%x ges=0x%x",
					display, nApplied, off.DwLocalPlayerPawn, off.DwEntityList, off.DwLocalPlayerController,
					off.DwViewMatrix, off.DwGameEntitySystem)
			}
			log.Printf("[CS2Mem:%d] dw_local_player_pawn still 0 after sigscanner — offsets from %q; update config/cs2_dumper (make cs2-offsets), libclient_offsets.json or sig patterns",
				display, sourceLabel)
		}
		return nil
	}

	if nApplied > 0 {
		log.Printf("[CS2Mem:%d] sigscanner applied %d offsets: dwPawn=0x%x ent=0x%x ctrl=0x%x vm=0x%x ges=0x%x",
			display, nApplied, off.DwLocalPlayerPawn, off.DwEntityList, off.DwLocalPlayerController,
			off.DwViewMatrix, off.DwGameEntitySystem)
	}

	m := &linuxCS2Mem{
		pid:         pid,
		clientBase:  base,
		selPath:     selPath,
		off:         off,
		sourceLabel: sourceLabel,
		display:     display,
	}
	key := fmt.Sprintf("%d:%d:0x%x", display, pid, base)
	now := time.Now()
	linuxMemInitLogMu.Lock()
	if prev, ok := linuxMemInitLogAt[key]; !ok || now.Sub(prev) >= 30*time.Second {
		linuxMemInitLogAt[key] = now
		linuxMemInitLogMu.Unlock()
		log.Printf("[CS2Mem:%d] init OK pid=%d imageBase=0x%x module=%q source=%q dwPawn=0x%x origin=0x%x eye=0x%x vel=0x%x ent=0x%x ctrl=0x%x m_hPawn=0x%x",
			display, pid, base, selPath, sourceLabel, off.DwLocalPlayerPawn, off.MvOldOrigin, off.MangEyeAngles, off.MvecAbsVelocity,
			off.DwEntityList, off.DwLocalPlayerController, off.MHPlayerPawn)
	} else {
		linuxMemInitLogMu.Unlock()
	}
	maybeScheduleModuleMemoryDump(display, m)
	return m
}

func readProcPPid(pid int) int {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				p, err := strconv.Atoi(fields[1])
				if err == nil {
					return p
				}
			}
			break
		}
	}
	return -1
}

func environHasDisplay(pid int, display int) bool {
	want := []byte(fmt.Sprintf("DISPLAY=:%d", display))
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, want)
}

// pidAssociatesWithDisplay: у cs2 часто нет DISPLAY в /proc/pid/environ — проверяем цепочку предков (Steam с тем же :N).
func pidAssociatesWithDisplay(pid int, display int) bool {
	if display < 0 {
		return true
	}
	if environHasDisplay(pid, display) {
		return true
	}
	seen := make(map[int]bool)
	p := readProcPPid(pid)
	for step := 0; step < 64 && p > 1; step++ {
		if seen[p] {
			break
		}
		seen[p] = true
		if environHasDisplay(p, display) {
			return true
		}
		p = readProcPPid(p)
	}
	return false
}

func cs2PIDForDisplay(display int) (int, bool) {
	pids := cs2PIDsLinux()
	if len(pids) > 1 && strings.TrimSpace(os.Getenv(envCS2PID)) != "" {
		log.Printf("[CS2Mem] %s игнорируется при нескольких процессах cs2 — привязка по DISPLAY/предкам", envCS2PID)
	} else if len(pids) <= 1 {
		if s := os.Getenv(envCS2PID); s != "" {
			p, err := strconv.Atoi(strings.TrimSpace(s))
			if err == nil && p > 0 {
				return p, true
			}
		}
	}
	if len(pids) == 0 {
		return 0, false
	}
	if display < 0 {
		p, err := strconv.Atoi(pids[0])
		return p, err == nil && p > 0
	}
	for _, pidStr := range pids {
		p, err := strconv.Atoi(pidStr)
		if err != nil || p <= 0 {
			continue
		}
		if pidAssociatesWithDisplay(p, display) {
			return p, true
		}
	}
	if len(pids) == 1 {
		p, err := strconv.Atoi(pids[0])
		if err == nil && p > 0 {
			log.Printf("[CS2Mem] DISPLAY=:%d не найден в цепочке процессов; используем единственный pid %d", display, p)
			return p, true
		}
	}
	log.Printf("[CS2Mem] no cs2 for DISPLAY=:%d among candidates %v", display, pids)
	return 0, false
}

// moduleImageBase: lowest mapped vaddr for the client module. Dump RVAs are relative to this, not only the r-xp text line.
func moduleImageBase(pid int, off cs2MemoryJSON) (base uint64, path string, err error) {
	mapPath := filepath.Join("/proc", strconv.Itoa(pid), "maps")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return 0, "", err
	}
	sub := strings.TrimSpace(off.ModuleSubstr)
	if sub == "" {
		sub = "libclient"
	}
	needlePath := strings.TrimSpace(off.ModulePathContains)

	var minAddr uint64 = math.MaxUint64
	var chosenPath string
	for _, line := range strings.Split(string(data), "\n") {
		st, _, _, mpath, ok := parseMapsLine(line)
		if !ok || mpath == "" {
			continue
		}
		if strings.HasPrefix(mpath, "[") {
			continue
		}
		if strings.Contains(strings.ToLower(mpath), "steamclient") {
			continue
		}
		if !strings.Contains(mpath, sub) {
			continue
		}
		if needlePath != "" && !strings.Contains(mpath, needlePath) {
			continue
		}
		if st < minAddr {
			minAddr = st
			chosenPath = mpath
		}
	}
	if minAddr == math.MaxUint64 || chosenPath == "" {
		return 0, "", fmt.Errorf("no mapping contains %q", sub)
	}
	return minAddr, chosenPath, nil
}

func parseMapsLine(line string) (start, end uint64, perms, path string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return 0, 0, "", "", false
	}
	rng := strings.Split(fields[0], "-")
	if len(rng) != 2 {
		return 0, 0, "", "", false
	}
	st, e1 := strconv.ParseUint(rng[0], 16, 64)
	en, e2 := strconv.ParseUint(rng[1], 16, 64)
	if e1 != nil || e2 != nil {
		return 0, 0, "", "", false
	}
	perms = fields[1]
	path = strings.Join(fields[5:], " ")
	return st, en, perms, path, true
}

func logGrepHintMaps(pid int, substr string) {
	mapPath := filepath.Join("/proc", strconv.Itoa(pid), "maps")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(strings.ToLower(line), "client") || strings.Contains(line, substr) {
			log.Printf("[CS2Mem] maps pid=%d: %s", pid, strings.TrimSpace(line))
			n++
			if n >= 12 {
				break
			}
		}
	}
}

func readProcMem(pid int, addr uint64, dst []byte) error {
	if len(dst) == 0 {
		return nil
	}
	liov := []unix.Iovec{{Base: &dst[0], Len: uint64(len(dst))}}
	riov := []unix.RemoteIovec{{Base: uintptr(addr), Len: len(dst)}}
	n, err := unix.ProcessVMReadv(pid, liov, riov, 0)
	if err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			return fmt.Errorf("%w (process_vm_readv denied)", err)
		}
		return err
	}
	if n != len(dst) {
		return fmt.Errorf("short read %d/%d", n, len(dst))
	}
	return nil
}

func ptrOK(u uint64) bool {
	return u >= 0x10000 && u < 0x7fffffffffff
}

func readU64Proc(pid int, addr uint64) (uint64, error) {
	var b [8]byte
	if err := readProcMem(pid, addr, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func readU32Proc(pid int, addr uint64) (uint32, error) {
	var b [4]byte
	if err := readProcMem(pid, addr, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

// CS2 externals: paged entity list; chunk stride 0x70 (see a2x dump era).
func entityPtrFromHandle(pid int, entityList uint64, handle uint32) (uint64, error) {
	if handle == 0 || handle == 0xffffffff {
		return 0, fmt.Errorf("invalid entity handle 0x%x", handle)
	}
	idx := handle & 0x7fff
	chunk := idx >> 9
	listEntry, err := readU64Proc(pid, entityList+uint64(8*chunk)+0x10)
	if err != nil {
		return 0, err
	}
	if !ptrOK(listEntry) {
		return 0, fmt.Errorf("invalid list chunk ptr 0x%x", listEntry)
	}
	pawn, err := readU64Proc(pid, listEntry+0x70*uint64(idx&0x1ff))
	if err != nil {
		return 0, err
	}
	if !ptrOK(pawn) {
		return 0, fmt.Errorf("invalid resolved pawn ptr 0x%x", pawn)
	}
	return pawn, nil
}

func (m *linuxCS2Mem) resolveLocalPawn() (uint64, error) {
	var pawnBuf [8]byte
	if err := readProcMem(m.pid, m.clientBase+m.off.DwLocalPlayerPawn, pawnBuf[:]); err != nil {
		return 0, err
	}
	pawn := binary.LittleEndian.Uint64(pawnBuf[:])
	if ptrOK(pawn) {
		return pawn, nil
	}
	if m.off.DwEntityList == 0 || m.off.DwLocalPlayerController == 0 || m.off.MHPlayerPawn == 0 {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x", pawn)
	}
	entityList, err := readU64Proc(m.pid, m.clientBase+m.off.DwEntityList)
	if err != nil {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; entity list: %w", pawn, err)
	}
	if !ptrOK(entityList) {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; invalid entity list ptr 0x%x", pawn, entityList)
	}
	ctrl, err := readU64Proc(m.pid, m.clientBase+m.off.DwLocalPlayerController)
	if err != nil {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; controller: %w", pawn, err)
	}
	if !ptrOK(ctrl) {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; invalid controller ptr 0x%x", pawn, ctrl)
	}
	h, err := readU32Proc(m.pid, ctrl+m.off.MHPlayerPawn)
	if err != nil {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; pawn handle: %w", pawn, err)
	}
	p2, err := entityPtrFromHandle(m.pid, entityList, h)
	if err != nil {
		return 0, fmt.Errorf("invalid pawn ptr 0x%x; entity resolve: %w", pawn, err)
	}
	return p2, nil
}

func (m *linuxCS2Mem) snapshot() (cs2MemSnapshot, error) {
	var out cs2MemSnapshot
	pawn, err := m.resolveLocalPawn()
	if err != nil {
		return out, err
	}
	var orig [12]byte
	if err := readProcMem(m.pid, pawn+m.off.MvOldOrigin, orig[:]); err != nil {
		return out, err
	}
	out.X = float64(math.Float32frombits(binary.LittleEndian.Uint32(orig[0:4])))
	out.Y = float64(math.Float32frombits(binary.LittleEndian.Uint32(orig[4:8])))
	out.Z = float64(math.Float32frombits(binary.LittleEndian.Uint32(orig[8:12])))

	if m.off.MangEyeAngles != 0 {
		var ang [8]byte
		if err := readProcMem(m.pid, pawn+m.off.MangEyeAngles, ang[:]); err == nil {
			out.Pitch = float64(math.Float32frombits(binary.LittleEndian.Uint32(ang[0:4])))
			out.Yaw = float64(math.Float32frombits(binary.LittleEndian.Uint32(ang[4:8])))
			out.AnglesOK = true
		}
	}
	if m.off.MvecAbsVelocity != 0 {
		var vel [12]byte
		if err := readProcMem(m.pid, pawn+m.off.MvecAbsVelocity, vel[:]); err == nil {
			out.VelX = float64(math.Float32frombits(binary.LittleEndian.Uint32(vel[0:4])))
			out.VelY = float64(math.Float32frombits(binary.LittleEndian.Uint32(vel[4:8])))
			out.VelZ = float64(math.Float32frombits(binary.LittleEndian.Uint32(vel[8:12])))
			out.VelOK = true
		}
	}
	out.OK = true
	return out, nil
}

func entityStride(off cs2MemoryJSON) uint64 {
	if off.EntityListStride != 0 {
		return off.EntityListStride
	}
	return 0x70
}

func (m *linuxCS2Mem) readViewMatrix() ([16]float32, error) {
	var mat [16]float32
	if m.off.DwViewMatrix == 0 {
		return mat, errors.New("no dw_view_matrix")
	}
	var buf [64]byte
	if err := readProcMem(m.pid, m.clientBase+m.off.DwViewMatrix, buf[:]); err != nil {
		return mat, err
	}
	for i := 0; i < 16; i++ {
		mat[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4 : i*4+4]))
	}
	return mat, nil
}

// worldToScreenCS2 — row-major 4×4 × (x,y,z,1) (как в D3D row-vector * M).
// worldToScreenCS2ColMajor — column-major 4×4 (как часто лежит VP в памяти). Переключение: SFARM_CS2_MEM_MATRIX_LAYOUT=col.
func worldToScreenCS2(x, y, z float64, mat [16]float32, sw, sh int) (sx, sy float64, ok bool) {
	mw := float64(mat[12])*x + float64(mat[13])*y + float64(mat[14])*z + float64(mat[15])
	if mw < 0.001 {
		return 0, 0, false
	}
	clipX := float64(mat[0])*x + float64(mat[1])*y + float64(mat[2])*z + float64(mat[3])
	clipY := float64(mat[4])*x + float64(mat[5])*y + float64(mat[6])*z + float64(mat[7])
	ndcX := clipX / mw
	ndcY := clipY / mw
	sx = (float64(sw) * 0.5) * (1.0 + ndcX)
	sy = (float64(sh) * 0.5) * (1.0 - ndcY)
	if sx < -200 || sy < -200 || sx > float64(sw)+200 || sy > float64(sh)+200 {
		return 0, 0, false
	}
	return sx, sy, true
}

func worldToScreenCS2ColMajor(x, y, z float64, mat [16]float32, sw, sh int) (sx, sy float64, ok bool) {
	mw := float64(mat[3])*x + float64(mat[7])*y + float64(mat[11])*z + float64(mat[15])
	if mw < 0.001 {
		return 0, 0, false
	}
	clipX := float64(mat[0])*x + float64(mat[4])*y + float64(mat[8])*z + float64(mat[12])
	clipY := float64(mat[1])*x + float64(mat[5])*y + float64(mat[9])*z + float64(mat[13])
	ndcX := clipX / mw
	ndcY := clipY / mw
	sx = (float64(sw) * 0.5) * (1.0 + ndcX)
	sy = (float64(sh) * 0.5) * (1.0 - ndcY)
	if sx < -200 || sy < -200 || sx > float64(sw)+200 || sy > float64(sh)+200 {
		return 0, 0, false
	}
	return sx, sy, true
}

func espW2S(x, y, z float64, mat [16]float32, sw, sh int) (sx, sy float64, ok bool) {
	if memW2SColMajor {
		return worldToScreenCS2ColMajor(x, y, z, mat, sw, sh)
	}
	return worldToScreenCS2(x, y, z, mat, sw, sh)
}

func (m *linuxCS2Mem) entityPawnAtIndex(entityList uint64, index int) (uint64, error) {
	if index <= 0 {
		return 0, nil
	}
	stride := entityStride(m.off)
	idx := index & 0x7FFF
	chunk := idx >> 9
	ti := idx & 0x1FF
	listEntry, err := readU64Proc(m.pid, entityList+uint64(8*chunk)+0x10)
	if err != nil || !ptrOK(listEntry) {
		return 0, err
	}
	pawn, err := readU64Proc(m.pid, listEntry+stride*uint64(ti))
	if err != nil || !ptrOK(pawn) {
		return 0, err
	}
	return pawn, nil
}

func (m *linuxCS2Mem) espDets(vpW, vpH int) ([]YoloDet, error) {
	if m.off.DwViewMatrix == 0 || m.off.DwEntityList == 0 || m.off.MvOldOrigin == 0 ||
		m.off.MITeamNum == 0 || m.off.MIHealth == 0 || m.off.MLifeState == 0 {
		var miss []string
		if m.off.DwViewMatrix == 0 {
			miss = append(miss, "dw_view_matrix")
		}
		if m.off.DwEntityList == 0 {
			miss = append(miss, "dw_entity_list")
		}
		if m.off.MvOldOrigin == 0 {
			miss = append(miss, "m_v_old_origin")
		}
		if m.off.MITeamNum == 0 {
			miss = append(miss, "m_i_team_num")
		}
		if m.off.MIHealth == 0 {
			miss = append(miss, "m_i_health")
		}
		if m.off.MLifeState == 0 {
			miss = append(miss, "m_life_state")
		}
		logEspMissingOffsets(m.display, miss)
		return nil, nil
	}
	mat, err := m.readViewMatrix()
	if err != nil {
		return nil, nil
	}
	entityList, err := readU64Proc(m.pid, m.clientBase+m.off.DwEntityList)
	if err != nil || !ptrOK(entityList) {
		return nil, nil
	}
	localPawn, err := m.resolveLocalPawn()
	if err != nil {
		return nil, nil
	}
	var locOrig [12]byte
	if err := readProcMem(m.pid, localPawn+m.off.MvOldOrigin, locOrig[:]); err != nil {
		return nil, nil
	}
	px := float64(math.Float32frombits(binary.LittleEndian.Uint32(locOrig[0:4])))
	py := float64(math.Float32frombits(binary.LittleEndian.Uint32(locOrig[4:8])))
	pz := float64(math.Float32frombits(binary.LittleEndian.Uint32(locOrig[8:12])))

	ph := m.off.EspPlayerHeight
	if ph == 0 {
		ph = 72
	}

	localTeam, err := readU32Proc(m.pid, localPawn+m.off.MITeamNum)
	if err != nil {
		return nil, nil
	}
	var fwdX, fwdY, fwdZ float64
	anglesEspOK := false
	if m.off.MangEyeAngles != 0 {
		var ang [8]byte
		if readProcMem(m.pid, localPawn+m.off.MangEyeAngles, ang[:]) == nil {
			pitch := float64(math.Float32frombits(binary.LittleEndian.Uint32(ang[0:4])))
			yaw := float64(math.Float32frombits(binary.LittleEndian.Uint32(ang[4:8])))
			pitchRad := pitch * math.Pi / 180
			yawRad := yaw * math.Pi / 180
			cp := math.Cos(pitchRad)
			fwdX = cp * math.Cos(yawRad)
			fwdY = cp * math.Sin(yawRad)
			fwdZ = -math.Sin(pitchRad)
			anglesEspOK = true
		}
	}

	maxI := 512
	if m.off.DwGameEntitySystem != 0 && m.off.DwGameEntitySystemHighestIndex != 0 {
		ges, errG := readU64Proc(m.pid, m.clientBase+m.off.DwGameEntitySystem)
		if errG == nil && ptrOK(ges) {
			hi, errH := readU32Proc(m.pid, ges+m.off.DwGameEntitySystemHighestIndex)
			if errH == nil && hi > 0 && hi < 8192 {
				maxI = int(hi)
			}
		}
	}

	out := make([]YoloDet, 0, 16)
	const espMaxBoxes = 24
	for i := 1; i <= maxI && len(out) < espMaxBoxes; i++ {
		ent, err := m.entityPawnAtIndex(entityList, i)
		if err != nil || ent == 0 || ent == localPawn {
			continue
		}
		team, err := readU32Proc(m.pid, ent+m.off.MITeamNum)
		if err != nil || team == 0 || team == localTeam {
			continue
		}
		life, err := readU32Proc(m.pid, ent+m.off.MLifeState)
		if err != nil || life != 0 {
			continue
		}
		healthU, err := readU32Proc(m.pid, ent+m.off.MIHealth)
		if err != nil {
			continue
		}
		health := int32(healthU)
		if health <= 0 || health > 150 {
			continue
		}
		var eorig [12]byte
		if readProcMem(m.pid, ent+m.off.MvOldOrigin, eorig[:]) != nil {
			continue
		}
		ex := float64(math.Float32frombits(binary.LittleEndian.Uint32(eorig[0:4])))
		ey := float64(math.Float32frombits(binary.LittleEndian.Uint32(eorig[4:8])))
		ez := float64(math.Float32frombits(binary.LittleEndian.Uint32(eorig[8:12])))
		if math.IsNaN(ex) || math.IsNaN(ey) || math.IsNaN(ez) || ex*ex+ey*ey+ez*ez < 1 {
			continue
		}
		dx, dy, dz := ex-px, ey-py, ez-pz
		dlen := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if dlen < 1 || dlen > 8000 {
			continue
		}
		if anglesEspOK && !espSkipFwdDot {
			dot := (dx*fwdX + dy*fwdY + dz*fwdZ) / dlen
			if dot < espFwdDotMin {
				continue
			}
		}

		feetSX, feetSY, ok1 := espW2S(ex, ey, ez, mat, vpW, vpH)
		headSX, headSY, ok2 := espW2S(ex, ey, ez+ph, mat, vpW, vpH)
		if !ok1 || !ok2 {
			continue
		}
		yTop := math.Min(headSY, feetSY)
		yBot := math.Max(headSY, feetSY)
		h := yBot - yTop
		if h < 4 {
			continue
		}
		wBox := h * 0.42
		if wBox < 6 {
			wBox = 6
		}
		cx := (feetSX + headSX) * 0.5
		x1 := cx - wBox*0.5
		x2 := cx + wBox*0.5
		y1 := yTop
		y2 := yBot
		if x2 <= x1 || y2 <= y1 {
			continue
		}
		out = append(out, YoloDet{
			Cls:  "player",
			Conf: 0.88,
			Xyxy: []float64{x1, y1, x2, y2},
		})
	}
	return out, nil
}
