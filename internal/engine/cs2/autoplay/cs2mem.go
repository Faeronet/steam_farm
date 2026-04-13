package autoplay

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Env SFARM_CS2_MEM_CONFIG — path to JSON (see config/cs2_memory.example.json).
// Optional: SFARM_CS2_PID — numeric pid; otherwise display-matched cs2 like isCS2RunningOnDisplay.

const envCS2MemConfig = "SFARM_CS2_MEM_CONFIG"
const envCS2PID = "SFARM_CS2_PID"
const envCS2MemVerbose = "SFARM_CS2_MEM_VERBOSE"
const envCS2MemDiag = "SFARM_CS2_MEM_DIAG"
const envCS2MemDiagMS = "SFARM_CS2_MEM_DIAG_MS"
const envCS2MemModuleDump = "SFARM_CS2_MEM_MODULE_DUMP"
const envCS2MemModuleDumpMaxMB = "SFARM_CS2_MEM_MODULE_DUMP_MAX_MB"

// Локальный HTTP (только Linux): снимок чтения libclient без записи в лог-файлы.
// SFARM_CS2_MEM_DEBUG_HTTP=1|127.0.0.1:PORT; интервал SFARM_CS2_MEM_DEBUG_MS (по умолчанию 15.6).
// Опционально: SFARM_CS2_MEM_DEBUG_TOKEN, SFARM_CS2_MEM_DEBUG_DISPLAY.
const envCS2MemDebugHTTP = "SFARM_CS2_MEM_DEBUG_HTTP"
const envCS2MemDebugMS = "SFARM_CS2_MEM_DEBUG_MS"
const envCS2MemDebugToken = "SFARM_CS2_MEM_DEBUG_TOKEN"
const envCS2MemDebugDisplay = "SFARM_CS2_MEM_DEBUG_DISPLAY"
// Путь к JSON со списком RVA (по умолчанию <repo>/so/rva_table.json). См. scripts/so/export_rva_xlsx.py.
const envCS2MemRVATable = "SFARM_CS2_MEM_RVA_TABLE"

func cs2MemVerbose() bool {
	s := os.Getenv(envCS2MemVerbose)
	return s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}

// cs2MemDiagTarget: диагностика чтения памяти для подбора Linux RVA.
//   - SFARM_CS2_MEM_DIAG=1|true — JSON одной строкой в лог `[CS2Mem:N] diag {...}`.
//   - SFARM_CS2_MEM_DIAG=/path/cs2_mem_diag.jsonl — только дописывать JSONL (удобно прикрепить файл).
// Интервал по умолчанию 4 s. SFARM_CS2_MEM_DIAG_MS — дробные мс (15.6 или 15,6 ≈ один тик при 64 Hz).
func cs2MemDiagTarget() (logStdout bool, jsonlPath string) {
	s := strings.TrimSpace(os.Getenv(envCS2MemDiag))
	if s == "" {
		return false, ""
	}
	if s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes") {
		return true, ""
	}
	return false, s
}

func cs2MemDiagInterval() time.Duration {
	s := strings.TrimSpace(os.Getenv(envCS2MemDiagMS))
	if s == "" {
		return 4 * time.Second
	}
	s = strings.Replace(s, ",", ".", 1)
	ms, err := strconv.ParseFloat(s, 64)
	if err != nil || ms < 0 {
		return 4 * time.Second
	}
	// нижняя граница 0.5 мс — защита от опечаток вроде 0; для «каждый тик» обычно 15–16 мс
	if ms < 0.5 {
		ms = 0.5
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// cs2MemModuleDumpParentDir: не «вся RAM процесса» (ГБ мусора), а дамп libclient.so + полный maps.
// SFARM_CS2_MEM_MODULE_DUMP=1|true → каталоги в /tmp; иначе путь-родитель. Один раз на пару display+pid+module.
// Лимит размера: SFARM_CS2_MEM_MODULE_DUMP_MAX_MB (по умолчанию 512).
func cs2MemModuleDumpParentDir() string {
	s := strings.TrimSpace(os.Getenv(envCS2MemModuleDump))
	if s == "" {
		return ""
	}
	if s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes") {
		return "/tmp"
	}
	return s
}

func cs2MemModuleDumpMaxBytes() int64 {
	s := strings.TrimSpace(os.Getenv(envCS2MemModuleDumpMaxMB))
	if s == "" {
		return 512 * 1024 * 1024
	}
	mb, err := strconv.ParseInt(s, 10, 64)
	if err != nil || mb <= 0 {
		return 512 * 1024 * 1024
	}
	return mb * 1024 * 1024
}

// cs2MemDebugPollInterval: период опроса process_vm_readv для debug HTTP/SSE (~64 Hz по умолчанию).
func cs2MemDebugPollInterval() time.Duration {
	s := strings.TrimSpace(os.Getenv(envCS2MemDebugMS))
	if s == "" {
		return time.Duration(15.6 * float64(time.Millisecond))
	}
	s = strings.Replace(s, ",", ".", 1)
	ms, err := strconv.ParseFloat(s, 64)
	if err != nil || ms < 1 {
		return time.Duration(15.6 * float64(time.Millisecond))
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// ResolvedCS2MemConfigPath returns SFARM_CS2_MEM_CONFIG if set, else config/cs2_memory.json,
// else config/cs2_dumper when offsets.json+client_dll.json exist (см. make cs2-offsets).
func ResolvedCS2MemConfigPath() string {
	if p := strings.TrimSpace(os.Getenv(envCS2MemConfig)); p != "" {
		return p
	}
	root := autoplayRepoRoot()
	if root == "" {
		return ""
	}
	for _, name := range []string{"cs2_memory.json", "cs2_memory.local.json"} {
		p := filepath.Join(root, "config", name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	d := filepath.Join(root, "config", "cs2_dumper")
	if st1, e1 := os.Stat(filepath.Join(d, "offsets.json")); e1 == nil && !st1.IsDir() {
		if st2, e2 := os.Stat(filepath.Join(d, "client_dll.json")); e2 == nil && !st2.IsDir() {
			return d
		}
	}
	return ""
}

type cs2MemoryJSON struct {
	Doc                     string `json:"doc"`
	ModuleSubstr            string `json:"module_substr"`
	ModulePathContains      string `json:"module_path_contains"`
	// Начало секции .data: RVA от базы libclient (Ghidra Imagebase offset / колонка «Адрес» .data в readelf). Справочно для Ghidra; драйвер не читает память по этому полю.
	DataSectionStartRVA uint64 `json:"data_section_start_rva,omitempty"`
	// Следующая qword в .data (часто .data+20h от начала секции). Imagebase offset; только справочно.
	DataSectionPlus20hRVA uint64 `json:"data_section_plus_20h_rva,omitempty"`
	DwLocalPlayerPawn       uint64 `json:"dw_local_player_pawn"`
	DwEntityList            uint64 `json:"dw_entity_list"`
	DwLocalPlayerController uint64 `json:"dw_local_player_controller"`
	MHPlayerPawn            uint64 `json:"m_h_player_pawn"` // CCSPlayerController
	MvOldOrigin             uint64 `json:"m_v_old_origin"`
	MangEyeAngles           uint64 `json:"m_ang_eye_angles"`
	MvecAbsVelocity         uint64 `json:"m_vec_abs_velocity"`
	// ESP (оверлей без YOLO): dwViewMatrix + сущности; для Linux нужны корректные RVA под libclient.so.
	DwViewMatrix                   uint64  `json:"dw_view_matrix"`
	DwGameEntitySystem             uint64  `json:"dw_game_entity_system"`
	DwGameEntitySystemHighestIndex uint64  `json:"dw_game_entity_system_highest_index"` // оффсет от указателя GES
	MITeamNum                      uint64  `json:"m_i_team_num"`
	MIHealth                       uint64  `json:"m_i_health"`
	MLifeState                     uint64  `json:"m_life_state"`
	EntityListStride               uint64  `json:"entity_list_stride"` // 0 → 0x70
	EspPlayerHeight                float64 `json:"esp_player_height"`  // 0 → 72 (Z ноги → голова)
	EspEyeZOffset                  float64 `json:"esp_eye_z_offset"`   // 0 → 58 (над m_vOldOrigin для взгляда)
}

// offsetsNeedLibclientSigScanFill: хотя бы одно поле, которое applySigScanToOffsets может заполнить по .text, ещё ноль.
func offsetsNeedLibclientSigScanFill(off cs2MemoryJSON) bool {
	return off.DwLocalPlayerPawn == 0 ||
		off.DwEntityList == 0 ||
		off.DwLocalPlayerController == 0 ||
		off.DwViewMatrix == 0 ||
		off.DwGameEntitySystem == 0 ||
		off.DwGameEntitySystemHighestIndex == 0
}

func loadCS2MemConfig(path string) (cs2MemoryJSON, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cs2MemoryJSON{}, err
	}
	var j cs2MemoryJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return cs2MemoryJSON{}, err
	}
	if j.ModuleSubstr == "" {
		j.ModuleSubstr = "libclient"
	}
	return j, nil
}

// cs2MemSnapshot: world units and degrees for angles (engine conventions).
type cs2MemSnapshot struct {
	X, Y, Z          float64
	Pitch, Yaw       float64
	AnglesOK         bool
	VelX, VelY, VelZ float64
	VelOK            bool
	OK               bool
}

// snapshotWorldSane rejects obvious garbage from wrong base/offsets.
func snapshotWorldSane(s cs2MemSnapshot) bool {
	if !s.OK {
		return false
	}
	for _, v := range []float64{s.X, s.Y, s.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 120000 {
			return false
		}
	}
	return true
}

type cs2MemDriver interface {
	snapshot() (cs2MemSnapshot, error)
	// espDets: xyxy в пикселях клиента CS2 (vpW×vpH — реальный размер окна или fallback 1280×720), для PushYolo(0,0,w,h,…).
	espDets(vpW, vpH int) ([]YoloDet, error)
}

// pollCS2MemoryNav reads pawn pose from CS2 (Linux only when configured).
func (b *CS2Bot) pollCS2MemoryNav() {
	pollCS2MemImpl(b)
}

func (b *CS2Bot) ingestMemWorldPositionLocked(s cs2MemSnapshot) {
	if !s.OK || !snapshotWorldSane(s) {
		b.memNavActive = false
		return
	}
	now := time.Now()
	if s.VelOK {
		b.gsiVertVel = 0.58*b.gsiVertVel + 0.42*s.VelY
		b.gsiSampleTime = now
		b.gsiLastSampleY = s.Y
	} else {
		if !b.gsiSampleTime.IsZero() {
			dt := now.Sub(b.gsiSampleTime).Seconds()
			if dt > 0.04 {
				vy := (s.Y - b.gsiLastSampleY) / dt
				b.gsiVertVel = 0.55*b.gsiVertVel + 0.45*vy
			}
		}
		b.gsiSampleTime = now
		b.gsiLastSampleY = s.Y
	}
	b.ingestWorldXZLocked(s.X, s.Y, s.Z, now)
	b.memNavActive = true
	b.memAt = now
	b.memAnglesOK = s.AnglesOK
	if s.AnglesOK {
		b.memYawRad = s.Yaw * (math.Pi / 180.0)
	} else {
		b.memYawRad = 0
	}
	b.memSpeed2 = s.VelX*s.VelX + s.VelZ*s.VelZ
}
