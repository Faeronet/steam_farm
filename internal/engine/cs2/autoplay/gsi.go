package autoplay

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// GSI port used by all sandbox CS2 instances
const GSIPort = 30000

type GSIState struct {
	Provider struct {
		SteamID string `json:"steamid"`
		AppID   int    `json:"appid"`
	} `json:"provider"`
	Map *struct {
		Name  string `json:"name"`
		Mode  string `json:"mode"`
		Phase string `json:"phase"`
	} `json:"map"`
	Player *struct {
		SteamID  string `json:"steamid"`
		Activity string `json:"activity"` // "playing", "menu", "textinput"; в матче на сервере часто пусто
		// Team: строка или число (enum) — см. (*GSIState).normalizedTeam(); нужен player_team в GSI cfg.
		Team json.RawMessage `json:"team"`
		// World position from GSI when cfg enables player_position ("x, y, z").
		Position string `json:"position"`
		State    *struct {
			Health  int `json:"health"`
			Armor   int `json:"armor"`
			Flashed int `json:"flashed"`
			Burning int `json:"burning"`
			Money   int `json:"money"`
		} `json:"state"`
	} `json:"player"`
	Round *struct {
		Phase string `json:"phase"` // "live", "freezetime", "over"
	} `json:"round"`
}

// normalizedTeam — T/CT/Spectator/…; пусто если GSI не шлёт team или формат неизвестен.
func (g *GSIState) normalizedTeam() string {
	if g == nil || g.Player == nil {
		return ""
	}
	raw := g.Player.Team
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		switch int(f) {
		case 1:
			return "Spectator"
		case 2:
			return "T"
		case 3:
			return "CT"
		default:
			return ""
		}
	}
	return ""
}

type gsiAccountHandler struct {
	steamID string
	fn      func(*GSIState)
}

type GSIServer struct {
	server *http.Server
	mu     sync.RWMutex
	latest map[string]*GSIState // steamid -> latest state (last POST)
	// byAccount: один обработчик на аккаунт; маршрутизация по provider.steamid (не перезаписываем второй слот).
	byAccount map[int64]*gsiAccountHandler

	// gsiSteamBind: provider.steamid из POST -> account_id, если в БД steam_id пуст (несколько песочниц).
	gsiSteamBind   map[string]int64
	gsiSteamBindMu sync.Mutex

	// lazyRegOrder: порядок Register с пустым steam_id — привязка steam из GSI к боту в порядке запуска, не по account id.
	lazyRegMu    sync.Mutex
	lazyRegOrder []int64
}

func NewGSIServer() *GSIServer {
	gs := &GSIServer{
		latest:    make(map[string]*GSIState),
		byAccount: make(map[int64]*gsiAccountHandler),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", gs.handlePost)
	gs.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", GSIPort),
		Handler: mux,
	}
	return gs
}

func (gs *GSIServer) Start() {
	go func() {
		log.Printf("[GSI] Listening on %s", gs.server.Addr)
		if err := gs.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[GSI] Server error: %v", err)
		}
	}()
}

func (gs *GSIServer) Stop() {
	gs.server.Close()
}

// RegisterAccountHandler привязывает GSI к account_id. Пустой steam_id: ленивая привязка по первым POST (см. gsiSteamBind); для надёжности при 2+ ботах укажите steam_id в БД.
func (gs *GSIServer) RegisterAccountHandler(accountID int64, steamID string, fn func(*GSIState)) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if steamID == "" {
		nEmpty := 0
		for id, prev := range gs.byAccount {
			if id != accountID && prev != nil && strings.TrimSpace(prev.steamID) == "" {
				nEmpty++
			}
		}
		log.Printf("[GSI] account %d: steam_id в БД пуст — GSI привяжется по provider из CS2 (lazy-bind); при нескольких таких ботаx надёжнее задать steam_id в UI", accountID)
		if nEmpty >= 1 {
			log.Printf("[GSI] подсказка: уже есть другой бот без steam_id — заполните steam_id у обоих, иначе возможна путаница до первых POST")
		}
	}
	if steamID != "" {
		for id, prev := range gs.byAccount {
			if id != accountID && prev != nil && prev.steamID == steamID {
				log.Printf("[GSI] warning: steam_id %s уже зарегистрирован для account %d; дубли GSI для двух песочниц с одним логином неразличимы", steamID, id)
				break
			}
		}
	}
	gs.byAccount[accountID] = &gsiAccountHandler{steamID: steamID, fn: fn}
	if strings.TrimSpace(steamID) == "" {
		gs.lazyRegMu.Lock()
		gs.lazyRegOrder = append(gs.lazyRegOrder, accountID)
		gs.lazyRegMu.Unlock()
	}
}

func (gs *GSIServer) UnregisterAccountHandler(accountID int64) {
	gs.mu.Lock()
	delete(gs.byAccount, accountID)
	gs.mu.Unlock()

	gs.lazyRegMu.Lock()
	lro := gs.lazyRegOrder[:0]
	for _, id := range gs.lazyRegOrder {
		if id != accountID {
			lro = append(lro, id)
		}
	}
	gs.lazyRegOrder = lro
	gs.lazyRegMu.Unlock()

	gs.gsiSteamBindMu.Lock()
	if gs.gsiSteamBind != nil {
		for sid, aid := range gs.gsiSteamBind {
			if aid == accountID {
				delete(gs.gsiSteamBind, sid)
			}
		}
	}
	gs.gsiSteamBindMu.Unlock()
}

func (gs *GSIServer) GetState(steamID string) *GSIState {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.latest[steamID]
}

func (gs *GSIServer) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(200)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	var state GSIState
	if err := json.Unmarshal(body, &state); err != nil {
		w.WriteHeader(400)
		return
	}

	steamID := state.Provider.SteamID
	if steamID == "" && state.Player != nil {
		steamID = state.Player.SteamID
	}

	gs.mu.Lock()
	if steamID != "" {
		gs.latest[steamID] = &state
	}
	handlers := make(map[int64]*gsiAccountHandler, len(gs.byAccount))
	for id, h := range gs.byAccount {
		handlers[id] = h
	}
	gs.mu.Unlock()

	// 1) Точное совпадение provider/player steamid с полем в БД.
	matched := false
	for _, reg := range handlers {
		if reg == nil || reg.fn == nil {
			continue
		}
		sid := strings.TrimSpace(reg.steamID)
		if sid != "" && steamID != "" && sid == steamID {
			reg.fn(&state)
			matched = true
		}
	}
	if matched {
		w.WriteHeader(200)
		return
	}

	// 2) В БД steam_id пуст: ленивая привязка steam из GSI к account_id (несколько ботов на одном :30000).
	// Порядок мьютексов: сначала gs.mu, потом gsiSteamBindMu — как в UnregisterAccountHandler (без взаимной блокировки).
	if steamID == "" {
		w.WriteHeader(200)
		return
	}

	gs.mu.RLock()
	var emptyIDs []int64
	for aid, reg := range gs.byAccount {
		if reg != nil && strings.TrimSpace(reg.steamID) == "" {
			emptyIDs = append(emptyIDs, aid)
		}
	}
	gs.mu.RUnlock()
	emptySet := make(map[int64]bool)
	for _, id := range emptyIDs {
		emptySet[id] = true
	}
	gs.lazyRegMu.Lock()
	regOrder := append([]int64(nil), gs.lazyRegOrder...)
	gs.lazyRegMu.Unlock()
	var candidates []int64
	for _, aid := range regOrder {
		if emptySet[aid] {
			candidates = append(candidates, aid)
		}
	}
	if len(candidates) == 0 {
		candidates = emptyIDs
		sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	}

	var targetAcc int64
	var have bool
	gs.gsiSteamBindMu.Lock()
	if gs.gsiSteamBind == nil {
		gs.gsiSteamBind = make(map[string]int64)
	}
	targetAcc, have = gs.gsiSteamBind[steamID]
	if !have {
		usedAcc := make(map[int64]bool)
		for _, aid := range gs.gsiSteamBind {
			usedAcc[aid] = true
		}
		for _, aid := range candidates {
			if usedAcc[aid] {
				continue
			}
			gs.gsiSteamBind[steamID] = aid
			targetAcc = aid
			have = true
			log.Printf("[GSI] lazy-bind steam_id=%s -> account %d (порядок запуска ботов; steam_id в UI надёжнее)", steamID, aid)
			break
		}
	}
	gs.gsiSteamBindMu.Unlock()

	if have {
		gs.mu.RLock()
		reg := gs.byAccount[targetAcc]
		gs.mu.RUnlock()
		if reg != nil && reg.fn != nil {
			reg.fn(&state)
		}
	}
	w.WriteHeader(200)
}

// EnsureGSIConfig creates the GSI config file in the CS2 cfg directory.
// Since all sandboxes share the same steamapps via symlink, this only needs
// to be done once. The config tells CS2 to POST game state to our listener.
// Путь как у console.log (cs2CfgDir) — при запуске sfarm от root не использовать только /root/snap/...
func EnsureGSIConfig() error {
	cfgDir := cs2CfgDir()
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return fmt.Errorf("CS2 cfg directory: %w", err)
	}

	cfgPath := filepath.Join(cfgDir, "gamestate_integration_sfarm.cfg")

	config := fmt.Sprintf(`"sfarm_bot"
{
    "uri"       "http://127.0.0.1:%d/"
    "timeout"   "5.0"
    "buffer"    "0.1"
    "throttle"  "0.5"
    "heartbeat" "30.0"
    "output"
    {
        "precision_position" "1"
    }
    "data"
    {
        "provider"            "1"
        "map"                 "1"
        "round"               "1"
        "player_id"           "1"
        "player_state"        "1"
        "player_weapons"      "1"
        "player_match_stats"  "1"
        "player_position"     "1"
        "player_team"         "1"
    }
}
`, GSIPort)

	if err := os.WriteFile(cfgPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write GSI config: %w", err)
	}
	log.Printf("[GSI] Config written to %s", cfgPath)
	return nil
}
