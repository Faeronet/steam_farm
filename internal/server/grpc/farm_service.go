package grpc

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/faeronet/steam-farm-system/internal/database"
	farmproto "github.com/faeronet/steam-farm-system/internal/proto/farm"
	"github.com/faeronet/steam-farm-system/internal/server/ws"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type FarmService struct {
	mu      sync.RWMutex
	db      *database.DB
	hub     *ws.Hub
	clients map[string]*WorkerClient
}

type WorkerClient struct {
	ID   string
	Conn *websocket.Conn
	mu   sync.Mutex
}

type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func NewFarmService(db *database.DB, hub *ws.Hub) *FarmService {
	return &FarmService{
		db:      db,
		hub:     hub,
		clients: make(map[string]*WorkerClient),
	}
}

func (s *FarmService) HandleWorkerConnect(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[gRPC] Upgrade error: %v", err)
		return
	}

	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = "worker-" + r.RemoteAddr
	}

	worker := &WorkerClient{
		ID:   clientID,
		Conn: conn,
	}

	s.mu.Lock()
	s.clients[clientID] = worker
	s.mu.Unlock()

	log.Printf("[gRPC] Worker connected: %s", clientID)

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.clients, clientID)
		s.mu.Unlock()
		log.Printf("[gRPC] Worker disconnected: %s", clientID)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			continue
		}

		s.handleMessage(worker, env)
	}
}

func (s *FarmService) handleMessage(worker *WorkerClient, env Envelope) {
	switch env.Type {
	case "status":
		var report farmproto.StatusReport
		if err := json.Unmarshal(env.Payload, &report); err != nil {
			return
		}
		s.handleStatus(worker, report)

	case "drop":
		var report farmproto.DropReport
		if err := json.Unmarshal(env.Payload, &report); err != nil {
			return
		}
		s.handleDrop(worker, report)

	case "heartbeat":
		var hb farmproto.HeartbeatMsg
		if err := json.Unmarshal(env.Payload, &hb); err != nil {
			return
		}
		s.handleHeartbeat(worker, hb)

	case "get_accounts":
		s.handleGetAccounts(worker)
	}
}

func (s *FarmService) handleStatus(worker *WorkerClient, report farmproto.StatusReport) {
	_, _ = s.db.Pool.Exec(nil,
		`UPDATE accounts SET status = $1, status_detail = $2, cs2_level = $3, cs2_xp = $4, updated_at = NOW()
		 WHERE id = $5`,
		report.Status, report.Detail, report.Level, report.XP, report.AccountID)

	s.hub.Broadcast(ws.EventFarmStatus, report)
}

func (s *FarmService) handleDrop(worker *WorkerClient, report farmproto.DropReport) {
	_, _ = s.db.Pool.Exec(nil,
		`INSERT INTO drops (account_id, game_type, item_name, item_type, item_image_url, choice_options)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		report.AccountID, report.GameType, report.ItemName, report.ItemType, report.ItemImageURL, nil)

	if report.HasChoice {
		s.hub.Broadcast(ws.EventRewardPending, report)
	} else {
		s.hub.Broadcast(ws.EventDropReceived, report)
	}
}

func (s *FarmService) handleHeartbeat(worker *WorkerClient, hb farmproto.HeartbeatMsg) {
	log.Printf("[gRPC] Heartbeat from %s: %d bots, %d sandboxes",
		hb.ClientID, hb.ActiveBots, hb.ActiveSandboxes)
}

func (s *FarmService) handleGetAccounts(worker *WorkerClient) {
	rows, err := s.db.Pool.Query(nil,
		`SELECT id, username, game_type, farm_mode, status, shared_secret, identity_secret, proxy
		 FROM accounts ORDER BY id`)
	if err != nil {
		return
	}
	defer rows.Close()

	var accounts []farmproto.AccountInfo
	for rows.Next() {
		var a farmproto.AccountInfo
		var ss, is, proxy *string
		if err := rows.Scan(&a.ID, &a.Username, &a.GameType, &a.FarmMode, &a.Status, &ss, &is, &proxy); err != nil {
			continue
		}
		if ss != nil {
			a.SharedSecret = *ss
		}
		if is != nil {
			a.IdentitySecret = *is
		}
		if proxy != nil {
			a.Proxy = *proxy
		}
		accounts = append(accounts, a)
	}

	payload, _ := json.Marshal(accounts)
	worker.Send(Envelope{Type: "accounts", Payload: payload})
}

func (w *WorkerClient) Send(env Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Conn.WriteJSON(env)
}

func (s *FarmService) SendCommand(clientID string, cmd farmproto.CommandMsg) error {
	s.mu.RLock()
	worker, ok := s.clients[clientID]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	payload, _ := json.Marshal(cmd)
	return worker.Send(Envelope{Type: "command", Payload: payload})
}

func (s *FarmService) ConnectedWorkers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	return ids
}
