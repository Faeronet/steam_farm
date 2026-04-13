package autoplay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// resolveCS2MemoryOffsets: 1) SFARM_CS2_MEM_CONFIG / config/cs2_memory.json (ручной JSON),
// 2) config/cs2_dumper/offsets.json + client_dll.json (вывод a2x/cs2-dumper).
// На Linux после (2) применяется merge из so/libclient_offsets.json или config/libclient_offsets.json:
// там задают dw_* (RVA в libclient.so), не совпадающие с client.dll.
// Результат кэшируется на несколько секунд (не дергать диск на каждом тике телеметрии).
var memResolveCache struct {
	mu  sync.Mutex
	at  time.Time
	ttl time.Duration
	off cs2MemoryJSON
	src string
	err error
}

const memResolveCacheOK = 30 * time.Second
const memResolveCacheErr = 3 * time.Second

var linuxDumperRVAHint sync.Once

// libclientLinuxOverlay: тот же JSON-ключи, что у cs2_memory.json, плюс опциональный build_id (readelf -n libclient.so).
type libclientLinuxOverlay struct {
	BuildID string `json:"build_id,omitempty"`
	cs2MemoryJSON
}

func autoplayRepoRoot() string {
	exe, err := os.Executable()
	dir := "."
	if err == nil {
		dir = filepath.Dir(exe)
	}
	root := FindRepoRoot(dir)
	if root == "" {
		if wd, werr := os.Getwd(); werr == nil {
			root = FindRepoRoot(wd)
		}
	}
	return root
}

func resolveCS2MemoryOffsets() (cs2MemoryJSON, string, error) {
	now := time.Now()
	memResolveCache.mu.Lock()
	if !memResolveCache.at.IsZero() {
		ttl := memResolveCache.ttl
		if ttl == 0 {
			ttl = memResolveCacheOK
		}
		if now.Sub(memResolveCache.at) < ttl {
			o, s, e := memResolveCache.off, memResolveCache.src, memResolveCache.err
			memResolveCache.mu.Unlock()
			return o, s, e
		}
	}
	memResolveCache.mu.Unlock()

	off, src, err := resolveCS2MemoryOffsetsSlow()

	memResolveCache.mu.Lock()
	memResolveCache.at = now
	memResolveCache.off, memResolveCache.src, memResolveCache.err = off, src, err
	if err != nil {
		memResolveCache.ttl = memResolveCacheErr
	} else {
		memResolveCache.ttl = memResolveCacheOK
	}
	memResolveCache.mu.Unlock()
	return off, src, err
}

func resolveCS2MemoryOffsetsSlow() (cs2MemoryJSON, string, error) {
	if p := strings.TrimSpace(os.Getenv(envCS2MemConfig)); p != "" {
		j, err := loadCS2MemConfig(p)
		if err != nil {
			return cs2MemoryJSON{}, "", err
		}
		if j.DwLocalPlayerPawn != 0 && j.MvOldOrigin != 0 {
			return j, p, nil
		}
	}

	root := autoplayRepoRoot()
	if root != "" {
		for _, name := range []string{"cs2_memory.json", "cs2_memory.local.json"} {
			p := filepath.Join(root, "config", name)
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				continue
			}
			j, err := loadCS2MemConfig(p)
			if err != nil {
				continue
			}
			if j.DwLocalPlayerPawn != 0 && j.MvOldOrigin != 0 {
				return j, p, nil
			}
		}
		dumperDir := filepath.Join(root, "config", "cs2_dumper")
		if j, err := loadCS2MemFromDumperDir(dumperDir); err == nil && j.MvOldOrigin != 0 {
			overlayPath := mergeLibclientLinuxOverlay(root, &j)
			label := dumperDir
			if overlayPath != "" {
				label = dumperDir + "+" + overlayPath
				if j.DwLocalPlayerPawn == 0 {
					// overlay без pawn — разрешение невалидно
				} else {
					return j, label, nil
				}
			} else if runtime.GOOS == "linux" {
				// Глобальные dw_* в dumper — RVA client.dll (Windows), для libclient.so их нужно подобрать sigscan’ом.
				stripDumperGlobalRVAsForSigScan(&j)
				label = dumperDir + "+sigscan-pending"
				linuxDumperRVAHint.Do(func() {
					log.Printf("[CS2Mem] a2x/cs2-dumper: глобальные dw_* сброшены в 0 под sigscan libclient.so (после управляемого спавна)")
				})
				return j, label, nil
			} else if j.DwLocalPlayerPawn != 0 {
				return j, dumperDir, nil
			}
		}
	}

	return cs2MemoryJSON{}, "", fmt.Errorf("нет оффсетов: создайте config/cs2_memory.json или выполните «make cs2-offsets» (config/cs2_dumper/)")
}

// CS2MemoryResolvedLabel — краткая подпись источника оффсетов для логов/UI.
func CS2MemoryResolvedLabel() string {
	_, src, err := resolveCS2MemoryOffsets()
	if err != nil || src == "" {
		return ""
	}
	return src
}

func jsonFieldU64(m map[string]interface{}, key string) (uint64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case json.Number:
		u, err := strconv.ParseUint(string(x), 10, 64)
		return u, err == nil
	default:
		return 0, false
	}
}

func loadCS2MemFromDumperDir(dir string) (cs2MemoryJSON, error) {
	offPath := filepath.Join(dir, "offsets.json")
	cliPath := filepath.Join(dir, "client_dll.json")
	if _, err := os.Stat(offPath); err != nil {
		return cs2MemoryJSON{}, err
	}
	if _, err := os.Stat(cliPath); err != nil {
		return cs2MemoryJSON{}, err
	}
	offRaw, err := os.ReadFile(offPath)
	if err != nil {
		return cs2MemoryJSON{}, err
	}
	cliRaw, err := os.ReadFile(cliPath)
	if err != nil {
		return cs2MemoryJSON{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(offRaw))
	dec.UseNumber()
	var offRoot map[string]interface{}
	if err := dec.Decode(&offRoot); err != nil {
		return cs2MemoryJSON{}, fmt.Errorf("offsets.json: %w", err)
	}
	cl, ok := offRoot["client.dll"].(map[string]interface{})
	if !ok {
		return cs2MemoryJSON{}, fmt.Errorf("offsets.json: no client.dll")
	}
	dw, ok := jsonFieldU64(cl, "dwLocalPlayerPawn")
	if !ok || dw == 0 {
		return cs2MemoryJSON{}, fmt.Errorf("offsets.json: dwLocalPlayerPawn")
	}

	dec2 := json.NewDecoder(bytes.NewReader(cliRaw))
	dec2.UseNumber()
	var cliRoot map[string]interface{}
	if err := dec2.Decode(&cliRoot); err != nil {
		return cs2MemoryJSON{}, fmt.Errorf("client_dll.json: %w", err)
	}
	cd, ok := cliRoot["client.dll"].(map[string]interface{})
	if !ok {
		return cs2MemoryJSON{}, fmt.Errorf("client_dll.json: no client.dll")
	}
	classes, ok := cd["classes"].(map[string]interface{})
	if !ok {
		return cs2MemoryJSON{}, fmt.Errorf("client_dll.json: no classes")
	}

	origin, err := classFieldU64(classes, "C_BasePlayerPawn", "m_vOldOrigin")
	if err != nil {
		return cs2MemoryJSON{}, err
	}
	eye, err := classFieldU64(classes, "C_CSPlayerPawn", "m_angEyeAngles")
	if err != nil {
		return cs2MemoryJSON{}, err
	}
	vel, err := classFieldU64(classes, "C_BaseEntity", "m_vecAbsVelocity")
	if err != nil {
		return cs2MemoryJSON{}, err
	}

	var dwEnt, dwCtrl uint64
	if v, ok := jsonFieldU64(cl, "dwEntityList"); ok {
		dwEnt = v
	}
	if v, ok := jsonFieldU64(cl, "dwLocalPlayerController"); ok {
		dwCtrl = v
	}
	mhPawn, _ := classFieldU64(classes, "CCSPlayerController", "m_hPlayerPawn")

	teamN, _ := classFieldU64(classes, "C_BaseEntity", "m_iTeamNum")
	healthO, _ := classFieldU64(classes, "C_BaseEntity", "m_iHealth")
	lifeO, _ := classFieldU64(classes, "C_BaseEntity", "m_lifeState")

	var dwVM, dwGES, dwGESHi uint64
	if v, ok := jsonFieldU64(cl, "dwViewMatrix"); ok {
		dwVM = v
	}
	if v, ok := jsonFieldU64(cl, "dwGameEntitySystem"); ok {
		dwGES = v
	}
	if v, ok := jsonFieldU64(cl, "dwGameEntitySystem_highestEntityIndex"); ok {
		dwGESHi = v
	}

	out := cs2MemoryJSON{
		ModuleSubstr:                   "libclient",
		DwLocalPlayerPawn:              dw,
		DwEntityList:                   dwEnt,
		DwLocalPlayerController:        dwCtrl,
		MHPlayerPawn:                   mhPawn,
		MvOldOrigin:                    origin,
		MangEyeAngles:                  eye,
		MvecAbsVelocity:                vel,
		DwViewMatrix:                   dwVM,
		DwGameEntitySystem:             dwGES,
		DwGameEntitySystemHighestIndex: dwGESHi,
		MITeamNum:                      teamN,
		MIHealth:                       healthO,
		MLifeState:                     lifeO,
	}
	return out, nil
}

func classFieldU64(classes map[string]interface{}, class, field string) (uint64, error) {
	raw, ok := classes[class].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("client_dll: no class %s", class)
	}
	fields, ok := raw["fields"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("client_dll: %s has no fields", class)
	}
	u, ok := jsonFieldU64(fields, field)
	if !ok {
		return 0, fmt.Errorf("client_dll: %s.%s missing", class, field)
	}
	return u, nil
}

// mergeLibclientLinuxOverlay подставляет RVA libclient.so поверх значений из offsets.json (Windows).
// Поле 0 в файле означает «не менять». Ищет so/libclient_offsets.json, затем config/libclient_offsets.json.
// stripDumperGlobalRVAsForSigScan обнуляет глобальные указатели из offsets.json (Windows), чтобы их заново выставил sigscan по libclient.so.
func stripDumperGlobalRVAsForSigScan(j *cs2MemoryJSON) {
	if j == nil {
		return
	}
	j.DwLocalPlayerPawn = 0
	j.DwEntityList = 0
	j.DwLocalPlayerController = 0
	j.DwViewMatrix = 0
	j.DwGameEntitySystem = 0
	j.DwGameEntitySystemHighestIndex = 0
	SigScanInvalidateCache()
}

func mergeLibclientLinuxOverlay(repoRoot string, j *cs2MemoryJSON) string {
	if repoRoot == "" || j == nil || runtime.GOOS != "linux" {
		return ""
	}
	for _, p := range []string{
		filepath.Join(repoRoot, "so", "libclient_offsets.json"),
		filepath.Join(repoRoot, "config", "libclient_offsets.json"),
	} {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[CS2Mem] %s: %v", p, err)
			continue
		}
		var o libclientLinuxOverlay
		if err := json.Unmarshal(raw, &o); err != nil {
			log.Printf("[CS2Mem] %s: %v", p, err)
			continue
		}
		mergeCS2MemoryNonZero(j, o.cs2MemoryJSON)
		if o.BuildID != "" {
			log.Printf("[CS2Mem] Linux libclient overlay %s (build_id %s)", p, o.BuildID)
		} else {
			log.Printf("[CS2Mem] Linux libclient overlay %s", p)
		}
		return p
	}
	return ""
}

func mergeCS2MemoryNonZero(dst *cs2MemoryJSON, src cs2MemoryJSON) {
	if dst == nil {
		return
	}
	if strings.TrimSpace(src.ModuleSubstr) != "" {
		dst.ModuleSubstr = src.ModuleSubstr
	}
	if strings.TrimSpace(src.ModulePathContains) != "" {
		dst.ModulePathContains = src.ModulePathContains
	}
	if src.DwLocalPlayerPawn != 0 {
		dst.DwLocalPlayerPawn = src.DwLocalPlayerPawn
	}
	if src.DwEntityList != 0 {
		dst.DwEntityList = src.DwEntityList
	}
	if src.DwLocalPlayerController != 0 {
		dst.DwLocalPlayerController = src.DwLocalPlayerController
	}
	if src.MHPlayerPawn != 0 {
		dst.MHPlayerPawn = src.MHPlayerPawn
	}
	if src.MvOldOrigin != 0 {
		dst.MvOldOrigin = src.MvOldOrigin
	}
	if src.MangEyeAngles != 0 {
		dst.MangEyeAngles = src.MangEyeAngles
	}
	if src.MvecAbsVelocity != 0 {
		dst.MvecAbsVelocity = src.MvecAbsVelocity
	}
	if src.DwViewMatrix != 0 {
		dst.DwViewMatrix = src.DwViewMatrix
	}
	if src.DwGameEntitySystem != 0 {
		dst.DwGameEntitySystem = src.DwGameEntitySystem
	}
	if src.DwGameEntitySystemHighestIndex != 0 {
		dst.DwGameEntitySystemHighestIndex = src.DwGameEntitySystemHighestIndex
	}
	if src.MITeamNum != 0 {
		dst.MITeamNum = src.MITeamNum
	}
	if src.MIHealth != 0 {
		dst.MIHealth = src.MIHealth
	}
	if src.MLifeState != 0 {
		dst.MLifeState = src.MLifeState
	}
	if src.EntityListStride != 0 {
		dst.EntityListStride = src.EntityListStride
	}
	if src.EspPlayerHeight != 0 {
		dst.EspPlayerHeight = src.EspPlayerHeight
	}
	if src.EspEyeZOffset != 0 {
		dst.EspEyeZOffset = src.EspEyeZOffset
	}
	if src.DataSectionStartRVA != 0 {
		dst.DataSectionStartRVA = src.DataSectionStartRVA
	}
	if src.DataSectionPlus20hRVA != 0 {
		dst.DataSectionPlus20hRVA = src.DataSectionPlus20hRVA
	}
}
