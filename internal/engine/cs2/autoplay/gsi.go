package autoplay

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
		Activity string `json:"activity"` // "playing", "menu", "textinput"
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

// RegisterAccountHandler привязывает GSI к account_id. Нужен непустой steamID (как в gamestate provider),
// иначе POST не маршрутизируется (заполните steam_id у аккаунта в БД / UI).
func (gs *GSIServer) RegisterAccountHandler(accountID int64, steamID string, fn func(*GSIState)) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if steamID == "" {
		log.Printf("[GSI] account %d: steam_id пуст — GSI не будет доставляться этому боту; укажите steam_id аккаунта", accountID)
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
}

func (gs *GSIServer) UnregisterAccountHandler(accountID int64) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	delete(gs.byAccount, accountID)
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

	for _, reg := range handlers {
		if reg == nil || reg.fn == nil || reg.steamID == "" || reg.steamID != steamID {
			continue
		}
		reg.fn(&state)
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
    }
}
`, GSIPort)

	if err := os.WriteFile(cfgPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write GSI config: %w", err)
	}
	log.Printf("[GSI] Config written to %s", cfgPath)
	return nil
}
