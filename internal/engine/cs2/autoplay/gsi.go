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

type GSIServer struct {
	server   *http.Server
	mu       sync.RWMutex
	latest   map[string]*GSIState          // steamid -> latest state
	handlers map[string]func(*GSIState)    // steamid -> callback
}

func NewGSIServer() *GSIServer {
	gs := &GSIServer{
		latest:   make(map[string]*GSIState),
		handlers: make(map[string]func(*GSIState)),
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

func (gs *GSIServer) RegisterHandler(steamID string, fn func(*GSIState)) {
	gs.mu.Lock()
	gs.handlers[steamID] = fn
	gs.mu.Unlock()
}

func (gs *GSIServer) UnregisterHandler(steamID string) {
	gs.mu.Lock()
	delete(gs.handlers, steamID)
	delete(gs.latest, steamID)
	gs.mu.Unlock()
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
	if steamID == "" {
		w.WriteHeader(200)
		return
	}

	gs.mu.Lock()
	gs.latest[steamID] = &state
	handler := gs.handlers[steamID]
	gs.mu.Unlock()

	if handler != nil {
		handler(&state)
	}
	w.WriteHeader(200)
}

// EnsureGSIConfig creates the GSI config file in the CS2 cfg directory.
// Since all sandboxes share the same steamapps via symlink, this only needs
// to be done once. The config tells CS2 to POST game state to our listener.
func EnsureGSIConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cfgDir := filepath.Join(home, "snap/steam/common/.local/share/Steam/steamapps/common",
		"Counter-Strike Global Offensive/game/csgo/cfg")

	if _, err := os.Stat(cfgDir); os.IsNotExist(err) {
		return fmt.Errorf("CS2 cfg directory not found: %s", cfgDir)
	}

	cfgPath := filepath.Join(cfgDir, "gamestate_integration_sfarm.cfg")

	config := fmt.Sprintf(`"sfarm_bot"
{
    "uri"       "http://127.0.0.1:%d/"
    "timeout"   "5.0"
    "buffer"    "0.1"
    "throttle"  "0.5"
    "heartbeat" "30.0"
    "data"
    {
        "provider"            "1"
        "map"                 "1"
        "round"               "1"
        "player_id"           "1"
        "player_state"        "1"
        "player_weapons"      "1"
        "player_match_stats"  "1"
    }
}
`, GSIPort)

	if err := os.WriteFile(cfgPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write GSI config: %w", err)
	}
	log.Printf("[GSI] Config written to %s", cfgPath)
	return nil
}
