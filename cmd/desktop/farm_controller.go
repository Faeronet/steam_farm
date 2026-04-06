package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database"
	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/faeronet/steam-farm-system/internal/engine"
	"github.com/faeronet/steam-farm-system/internal/engine/sandbox"
	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

type FarmController struct {
	db         *database.DB
	bots       *engine.Manager
	sandboxes  *sandbox.Manager
	wsHub      *ws.Hub
	cfg        *common.ServerConfig
	logCapture *LogCapture
	appCtx     context.Context
}

func NewFarmController(appCtx context.Context, db *database.DB, bots *engine.Manager, sb *sandbox.Manager, hub *ws.Hub, cfg *common.ServerConfig, lc *LogCapture) *FarmController {
	fc := &FarmController{
		db:         db,
		bots:       bots,
		sandboxes:  sb,
		wsHub:      hub,
		cfg:        cfg,
		logCapture: lc,
		appCtx:     appCtx,
	}

	bots.SetStatusHandler(func(accountID int64, status models.AccountStatus, detail string) {
		_, _ = db.Pool.Exec(context.Background(),
			`UPDATE accounts SET status = $1, status_detail = $2, updated_at = NOW() WHERE id = $3`,
			string(status), detail, accountID)

		hub.Broadcast(ws.EventFarmStatus, map[string]interface{}{
			"account_id": accountID,
			"status":     status,
			"detail":     detail,
			"timestamp":  time.Now().Unix(),
		})
	})

	return fc
}

func (fc *FarmController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", fc.health)
	mux.HandleFunc("/api/accounts", fc.accounts)
	mux.HandleFunc("/api/accounts/", fc.accountByID)
	mux.HandleFunc("/api/farm/start", fc.startFarm)
	mux.HandleFunc("/api/farm/stop", fc.stopFarm)
	mux.HandleFunc("/api/farm/stop-all", fc.stopAll)
	mux.HandleFunc("/api/farm/status", fc.farmStatus)
	mux.HandleFunc("/api/sandbox/list", fc.sandboxList)
	mux.HandleFunc("/api/sandbox/launch", fc.sandboxLaunch)
	mux.HandleFunc("/api/sandbox/stop", fc.sandboxStop)
	mux.HandleFunc("/api/stats/dashboard", fc.dashboardStats)
	mux.HandleFunc("/api/drops", fc.drops)
	mux.HandleFunc("/api/drops/pending", fc.pendingDrops)
	mux.HandleFunc("/api/farm/sessions", fc.sessions)
	mux.HandleFunc("/api/logs", fc.logs)
}

func (fc *FarmController) health(w http.ResponseWriter, r *http.Request) {
	sandboxOK := fc.sandboxes != nil
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"version":      "0.1.0",
		"active_bots":  fc.bots.ActiveBots(),
		"sandbox_ok":   sandboxOK,
		"sandbox_count": 0,
	})
}

func (fc *FarmController) accounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		fc.createAccount(w, r)
		return
	}
	gameType := r.URL.Query().Get("game_type")
	query := `SELECT id, username, password_enc, shared_secret, identity_secret,
	          steam_id, avatar_url, persona_name, proxy, game_type,
	          farm_mode, status, status_detail, is_prime, cs2_level,
	          cs2_xp, cs2_xp_needed, cs2_rank, armory_stars, dota_hours,
	          last_drop_at, farmed_this_week, drop_collected, tags, group_name,
	          created_at, updated_at FROM accounts WHERE 1=1`
	args := []interface{}{}
	if gameType != "" {
		query += ` AND game_type = $1`
		args = append(args, gameType)
	}
	query += ` ORDER BY id ASC`

	rows, err := fc.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(
			&a.ID, &a.Username, &a.PasswordEnc, &a.SharedSecret, &a.IdentitySecret,
			&a.SteamID, &a.AvatarURL, &a.PersonaName, &a.Proxy, &a.GameType,
			&a.FarmMode, &a.Status, &a.StatusDetail, &a.IsPrime, &a.CS2Level,
			&a.CS2XP, &a.CS2XPNeeded, &a.CS2Rank, &a.ArmoryStars, &a.DotaHours,
			&a.LastDropAt, &a.FarmedThisWeek, &a.DropCollected, &a.Tags, &a.GroupName,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			log.Printf("[API] accounts scan error: %v", err)
			continue
		}
		accounts = append(accounts, a)
	}
	if accounts == nil {
		accounts = []models.Account{}
	}
	writeJSON(w, 200, accounts)
}

func (fc *FarmController) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		SharedSecret   string `json:"shared_secret"`
		IdentitySecret string `json:"identity_secret"`
		GameType       string `json:"game_type"`
		FarmMode       string `json:"farm_mode"`
		Proxy          string `json:"proxy"`
		GroupName      string `json:"group_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if req.FarmMode == "" {
		req.FarmMode = "protocol"
	}

	enc, err := common.Encrypt([]byte(req.Password), fc.cfg.EncryptionKey)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "encryption failed"})
		return
	}

	var id int64
	err = fc.db.Pool.QueryRow(r.Context(),
		`INSERT INTO accounts (username, password_enc, shared_secret, identity_secret, game_type, farm_mode, proxy, group_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		req.Username, enc, nilStr(req.SharedSecret), nilStr(req.IdentitySecret),
		req.GameType, req.FarmMode, nilStr(req.Proxy), nilStr(req.GroupName),
	).Scan(&id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]interface{}{"id": id})
}

func (fc *FarmController) accountByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/accounts/"), "/")
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	if r.Method == "DELETE" {
		fc.db.Pool.Exec(r.Context(), `DELETE FROM accounts WHERE id = $1`, id)
		writeJSON(w, 200, map[string]string{"status": "deleted"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// --- FARM CONTROL ---

type StartFarmRequest struct {
	AccountIDs []int64 `json:"account_ids"`
	Mode       string  `json:"mode"` // "protocol" or "sandbox"
	GameType   string  `json:"game_type"`
}

func (fc *FarmController) startFarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "POST only"})
		return
	}

	var req StartFarmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[Farm] Bad request: %v", err)
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	log.Printf("[Farm] Start request: accounts=%v mode=%s game=%s", req.AccountIDs, req.Mode, req.GameType)

	dbCtx := r.Context()
	botCtx := fc.appCtx
	results := make([]map[string]interface{}, 0, len(req.AccountIDs))

	for _, accID := range req.AccountIDs {
		var a models.Account
		var passwordEnc []byte
		err := fc.db.Pool.QueryRow(dbCtx,
			`SELECT id, username, password_enc, shared_secret, identity_secret,
			 steam_id, avatar_url, persona_name, proxy, game_type,
			 farm_mode, status, status_detail, is_prime, cs2_level,
			 cs2_xp, cs2_xp_needed, cs2_rank, armory_stars, dota_hours,
			 last_drop_at, farmed_this_week, drop_collected, tags, group_name,
			 created_at, updated_at FROM accounts WHERE id = $1`, accID,
		).Scan(
			&a.ID, &a.Username, &passwordEnc, &a.SharedSecret, &a.IdentitySecret,
			&a.SteamID, &a.AvatarURL, &a.PersonaName, &a.Proxy, &a.GameType,
			&a.FarmMode, &a.Status, &a.StatusDetail, &a.IsPrime, &a.CS2Level,
			&a.CS2XP, &a.CS2XPNeeded, &a.CS2Rank, &a.ArmoryStars, &a.DotaHours,
			&a.LastDropAt, &a.FarmedThisWeek, &a.DropCollected, &a.Tags, &a.GroupName,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			log.Printf("[Farm] Account #%d not found in DB: %v", accID, err)
			results = append(results, map[string]interface{}{"account_id": accID, "error": "not found: " + err.Error()})
			continue
		}
		a.PasswordEnc = passwordEnc

		log.Printf("[Farm] Account %s (id=%d): game=%s mode=%s", a.Username, a.ID, a.GameType, a.FarmMode)

		password, err := common.Decrypt(a.PasswordEnc, fc.cfg.EncryptionKey)
		if err != nil {
			log.Printf("[Farm] Failed to decrypt password for %s: %v", a.Username, err)
			results = append(results, map[string]interface{}{"account_id": accID, "error": "decrypt failed: " + err.Error()})
			continue
		}

		log.Printf("[Farm] Password decrypted for %s (len=%d)", a.Username, len(password))

		mode := req.Mode
		if mode == "" {
			mode = string(a.FarmMode)
		}

		effectiveGame := models.GameType(req.GameType)
		if effectiveGame == "" {
			effectiveGame = a.GameType
		}

		fc.db.Pool.Exec(dbCtx,
			`UPDATE accounts SET game_type = $1, updated_at = NOW() WHERE id = $2`,
			string(effectiveGame), accID)

		if mode == "sandbox" && fc.sandboxes != nil {
			log.Printf("[Farm] Launching sandbox for %s (%s)", a.Username, effectiveGame)
			info, err := fc.sandboxes.Launch(botCtx, a.ID, string(effectiveGame), a.Username, string(password))
			if err != nil {
				log.Printf("[Farm] Sandbox launch failed for %s: %v", a.Username, err)
				results = append(results, map[string]interface{}{"account_id": accID, "error": err.Error()})
				continue
			}
			fc.db.Pool.Exec(dbCtx,
				`INSERT INTO sandboxes (account_id, container_id, container_name, game_type, machine_id, hostname, display, vnc_port, status)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'running')
				 ON CONFLICT (account_id) DO UPDATE SET container_id=$2, status='running', updated_at=NOW()`,
				a.ID, info.ID, info.Name, info.GameType, info.MachineID, info.Hostname, info.Display, info.VNCPort)

			fc.db.Pool.Exec(dbCtx,
				`UPDATE accounts SET status='farming', status_detail=$1, updated_at=NOW() WHERE id=$2`,
				"sandbox: "+info.Name, a.ID)

			fc.wsHub.Broadcast(ws.EventFarmStatus, map[string]interface{}{
				"account_id": a.ID, "status": "farming", "detail": "sandbox: " + info.Name,
			})
			fc.wsHub.Broadcast(ws.EventSandboxChange, map[string]interface{}{
				"account_id": a.ID, "container": info.Name, "status": "running", "vnc_port": info.VNCPort,
			})
			log.Printf("[Farm] Sandbox started for %s: container=%s vnc=%d", a.Username, info.Name, info.VNCPort)
			results = append(results, map[string]interface{}{"account_id": accID, "mode": "sandbox", "container": info.Name, "vnc_port": info.VNCPort})
		} else {
			log.Printf("[Farm] Starting protocol bot for %s (game=%s, mode=%s)", a.Username, effectiveGame, mode)
			if err := fc.bots.StartBot(botCtx, a, string(password), effectiveGame); err != nil {
				log.Printf("[Farm] Bot start failed for %s: %v", a.Username, err)
				results = append(results, map[string]interface{}{"account_id": accID, "error": err.Error()})
				continue
			}
			log.Printf("[Farm] Bot started for %s — connecting to Steam CM...", a.Username)
			results = append(results, map[string]interface{}{"account_id": accID, "mode": mode, "status": "starting"})
		}
	}

	log.Printf("[Farm] Start complete: %d results", len(results))
	writeJSON(w, 200, map[string]interface{}{"results": results})
}

func (fc *FarmController) stopFarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "POST only"})
		return
	}

	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	for _, accID := range req.AccountIDs {
		_ = fc.bots.StopBot(accID)
		if fc.sandboxes != nil {
			_ = fc.sandboxes.Stop(ctx, accID)
		}
		fc.db.Pool.Exec(ctx, `UPDATE accounts SET status='idle', status_detail='stopped', updated_at=NOW() WHERE id=$1`, accID)
		fc.db.Pool.Exec(ctx, `UPDATE sandboxes SET status='stopped', updated_at=NOW() WHERE account_id=$1`, accID)
	}

	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

func (fc *FarmController) stopAll(w http.ResponseWriter, r *http.Request) {
	fc.bots.StopAll()
	if fc.sandboxes != nil {
		fc.sandboxes.StopAll(r.Context())
	}
	fc.db.Pool.Exec(r.Context(), `UPDATE accounts SET status='idle', status_detail='stopped by user', updated_at=NOW() WHERE status IN ('farming','queued')`)
	fc.db.Pool.Exec(r.Context(), `UPDATE sandboxes SET status='stopped', updated_at=NOW() WHERE status='running'`)
	writeJSON(w, 200, map[string]string{"status": "all stopped"})
}

func (fc *FarmController) farmStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var total, farming, idle, errored int
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&total)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status='farming'`).Scan(&farming)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status='idle'`).Scan(&idle)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status='error'`).Scan(&errored)

	sandboxCount := 0
	if fc.sandboxes != nil {
		sandboxCount = fc.sandboxes.ActiveCount()
	}

	writeJSON(w, 200, map[string]interface{}{
		"total":           total,
		"farming":         farming,
		"idle":            idle,
		"errored":         errored,
		"active_bots":     fc.bots.ActiveBots(),
		"active_sandboxes": sandboxCount,
	})
}

// --- SANDBOX ---

func (fc *FarmController) sandboxList(w http.ResponseWriter, r *http.Request) {
	if fc.sandboxes == nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	containers := fc.sandboxes.List()
	writeJSON(w, 200, containers)
}

func (fc *FarmController) sandboxLaunch(w http.ResponseWriter, r *http.Request) {
	if fc.sandboxes == nil {
		writeJSON(w, 503, map[string]string{"error": "sandbox manager not available"})
		return
	}
	var req struct {
		AccountID int64  `json:"account_id"`
		GameType  string `json:"game_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	var username string
	var passwordEnc []byte
	fc.db.Pool.QueryRow(r.Context(),
		`SELECT username, password_enc FROM accounts WHERE id=$1`, req.AccountID,
	).Scan(&username, &passwordEnc)

	pass, err := common.Decrypt(passwordEnc, fc.cfg.EncryptionKey)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "decrypt failed"})
		return
	}

	info, err := fc.sandboxes.Launch(r.Context(), req.AccountID, req.GameType, username, string(pass))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, info)
}

func (fc *FarmController) sandboxStop(w http.ResponseWriter, r *http.Request) {
	if fc.sandboxes == nil {
		writeJSON(w, 503, map[string]string{"error": "sandbox manager not available"})
		return
	}
	var req struct {
		AccountID int64 `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := fc.sandboxes.Stop(r.Context(), req.AccountID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

// --- STATS ---

func (fc *FarmController) dashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var totalAccounts, farmingAccounts, dropsWeek, pendingRewards int
	var weeklyRevenue float32
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&totalAccounts)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status='farming'`).Scan(&farmingAccounts)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM drops WHERE dropped_at > NOW() - INTERVAL '7 days'`).Scan(&dropsWeek)
	fc.db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(market_price),0) FROM drops WHERE dropped_at > NOW() - INTERVAL '7 days'`).Scan(&weeklyRevenue)
	fc.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM drops WHERE claimed=FALSE AND choice_options IS NOT NULL`).Scan(&pendingRewards)

	sandboxCount := 0
	if fc.sandboxes != nil {
		sandboxCount = fc.sandboxes.ActiveCount()
	}

	writeJSON(w, 200, map[string]interface{}{
		"total_accounts":   totalAccounts,
		"farming_accounts": farmingAccounts,
		"drops_this_week":  dropsWeek,
		"weekly_revenue":   weeklyRevenue,
		"pending_rewards":  pendingRewards,
		"active_sandboxes": sandboxCount,
		"active_bots":      fc.bots.ActiveBots(),
	})
}

func (fc *FarmController) drops(w http.ResponseWriter, r *http.Request) {
	rows, err := fc.db.Pool.Query(r.Context(),
		`SELECT id, account_id, session_id, game_type, item_name, item_type,
		 item_image_url, market_price, dropped_at, claimed, sent_to_trade,
		 choice_options, chosen_items FROM drops ORDER BY dropped_at DESC LIMIT 200`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var drops []map[string]interface{}
	for rows.Next() {
		var id, accountID int64
		var sessionID *int64
		var gameType, itemName string
		var itemType, imageURL *string
		var price *float32
		var droppedAt interface{}
		var claimed, sentToTrade bool
		var choiceOpts, chosenItems interface{}
		if err := rows.Scan(&id, &accountID, &sessionID, &gameType, &itemName,
			&itemType, &imageURL, &price, &droppedAt, &claimed, &sentToTrade,
			&choiceOpts, &chosenItems); err != nil {
			continue
		}
		drops = append(drops, map[string]interface{}{
			"id": id, "account_id": accountID, "game_type": gameType,
			"item_name": itemName, "item_type": itemType, "dropped_at": droppedAt,
			"claimed": claimed, "market_price": price, "choice_options": choiceOpts,
		})
	}
	if drops == nil {
		drops = []map[string]interface{}{}
	}
	writeJSON(w, 200, drops)
}

func (fc *FarmController) pendingDrops(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, []interface{}{})
}

func (fc *FarmController) sessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req StartFarmRequest
		json.NewDecoder(r.Body).Decode(&req)
		fc.startFarmInternal(r.Context(), req)
		writeJSON(w, 201, map[string]string{"status": "started"})
		return
	}
	writeJSON(w, 200, []interface{}{})
}

func (fc *FarmController) startFarmInternal(ctx context.Context, req StartFarmRequest) {
	// reuse startFarm logic
}

// --- MONITOR ---

func (fc *FarmController) RunMonitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fc.broadcastStatus(ctx)
		}
	}
}

func (fc *FarmController) broadcastStatus(ctx context.Context) {
	playtimes := fc.bots.GetPlaytimes()
	for accID := range playtimes {
		fc.db.Pool.Exec(ctx,
			`UPDATE accounts SET status = 'farming', dota_hours = dota_hours + 0.00139, updated_at = NOW()
			 WHERE id = $1`, accID)
	}

	rows, _ := fc.db.Pool.Query(ctx,
		`SELECT id, username, status, status_detail, game_type, cs2_level, cs2_xp, cs2_xp_needed, dota_hours
		 FROM accounts WHERE status IN ('farming','queued','error')`)
	if rows == nil {
		return
	}
	defer rows.Close()

	var updates []map[string]interface{}
	for rows.Next() {
		var id int64
		var username, status, gameType string
		var detail *string
		var level, xp, xpNeeded int
		var dotaHours float32
		rows.Scan(&id, &username, &status, &detail, &gameType, &level, &xp, &xpNeeded, &dotaHours)
		updates = append(updates, map[string]interface{}{
			"account_id":    id,
			"username":      username,
			"status":        status,
			"status_detail": detail,
			"game_type":     gameType,
			"cs2_level":     level,
			"cs2_xp":        xp,
			"dota_hours":    dotaHours,
		})
	}

	if len(updates) > 0 {
		fc.wsHub.Broadcast(ws.EventFarmStatus, updates)
	}

	if fc.sandboxes != nil && fc.sandboxes.ActiveCount() > 0 {
		fc.wsHub.Broadcast(ws.EventSandboxChange, fc.sandboxes.List())
	}
}

// --- LOGS ---

func (fc *FarmController) logs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 {
			n = v
		}
	}
	entries := fc.logCapture.Recent(n)
	if entries == nil {
		entries = []LogEntry{}
	}
	writeJSON(w, 200, entries)
}

// --- HELPERS ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
