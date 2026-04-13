//go:build linux

package autoplay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rvaTableJSON struct {
	Doc     string          `json:"doc"`
	Count   int             `json:"count"`
	Entries []rvaTableEntry `json:"entries"`
}

type rvaTableEntry struct {
	RvaHex string `json:"rva_hex"`
	Rva    uint64 `json:"rva"`
	Block  string `json:"block"`
}

type rvaProbeRow struct {
	RvaHex string `json:"rva_hex"`
	Rva    uint64 `json:"rva"`
	Block  string `json:"block"`
	U64Hex string `json:"raw_u64_hex,omitempty"`
	PtrOK  *bool  `json:"ptr_ok,omitempty"`
	Err    string `json:"err,omitempty"`
}

type rvaProbeResponse struct {
	OK                  bool          `json:"ok"`
	Err                 string        `json:"err,omitempty"`
	TsMs                int64         `json:"ts_ms"`
	PID                 int           `json:"pid"`
	ClientBase          string        `json:"client_base"`
	TablePath           string        `json:"table_path"`
	TableTotalEntries   int           `json:"table_total_entries"`
	FilterBlock         string        `json:"filter_block,omitempty"`
	MatchedEntries      int           `json:"matched_entries"`
	Offset              int           `json:"offset"`
	Limit               int           `json:"limit"`
	PtrOKCountInBatch   int           `json:"ptr_ok_count_in_batch"`
	Probes              []rvaProbeRow `json:"probes"`
}

var (
	rvaTableMu     sync.RWMutex
	rvaTableCached *rvaTableJSON
	rvaTablePath   string
)

func loadRVATableForProbe() (*rvaTableJSON, string, error) {
	p := filepath.Join(autoplayRepoRoot(), "so", "rva_table.json")
	if e := strings.TrimSpace(os.Getenv(envCS2MemRVATable)); e != "" {
		p = e
	}
	rvaTableMu.Lock()
	defer rvaTableMu.Unlock()
	if rvaTableCached != nil && rvaTablePath == p {
		return rvaTableCached, p, nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, p, err
	}
	var tab rvaTableJSON
	if err := json.Unmarshal(raw, &tab); err != nil {
		return nil, p, err
	}
	rvaTableCached, rvaTablePath = &tab, p
	return rvaTableCached, p, nil
}

func memDebugHandleRVAProbe(w http.ResponseWriter, r *http.Request, token string) {
	if !memDebugCheckToken(w, r, token) {
		return
	}
	m := memDebugTakeDriver()
	if m == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		b, _ := json.Marshal(rvaProbeResponse{OK: false, Err: "no active mem driver (start game + bot)"})
		_, _ = w.Write(b)
		return
	}
	q := r.URL.Query()
	force := strings.EqualFold(q.Get("force"), "1") || strings.EqualFold(q.Get("force"), "true")
	if memMatchGateEnabled() && !force && !memDataCollectionActive(m.display) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		b, _ := json.Marshal(rvaProbeResponse{
			OK:  false,
			Err: "match_collect_inactive (wait until controllable pawn in match; or ?force=1; or SFARM_CS2_MEM_MATCH_GATE=0)",
		})
		_, _ = w.Write(b)
		return
	}
	tab, path, err := loadRVATableForProbe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	if limit > 8000 {
		limit = 8000
	}
	off, _ := strconv.Atoi(q.Get("offset"))
	if off < 0 {
		off = 0
	}
	blockWant := strings.TrimSpace(q.Get("block"))

	var filtered []rvaTableEntry
	for _, e := range tab.Entries {
		if blockWant != "" && !strings.EqualFold(e.Block, blockWant) {
			continue
		}
		filtered = append(filtered, e)
	}

	resp := rvaProbeResponse{
		OK:                true,
		TsMs:              time.Now().UnixMilli(),
		PID:               m.pid,
		ClientBase:        fmt.Sprintf("0x%x", m.clientBase),
		TablePath:         path,
		TableTotalEntries: tab.Count,
		FilterBlock:       blockWant,
		MatchedEntries:    len(filtered),
		Offset:            off,
		Limit:             limit,
		Probes:            make([]rvaProbeRow, 0, limit),
	}

	pid, base := m.pid, m.clientBase
	for i := off; i < len(filtered) && len(resp.Probes) < limit; i++ {
		e := filtered[i]
		row := rvaProbeRow{RvaHex: e.RvaHex, Rva: e.Rva, Block: e.Block}
		u, err := readU64Proc(pid, base+e.Rva)
		if err != nil {
			row.Err = err.Error()
		} else {
			row.U64Hex = fmt.Sprintf("0x%x", u)
			ok := ptrOK(u)
			row.PtrOK = &ok
			if ok {
				resp.PtrOKCountInBatch++
			}
		}
		resp.Probes = append(resp.Probes, row)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}
