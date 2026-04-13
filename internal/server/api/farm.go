package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// DesktopStartFarmRequest — тело POST /api/farm/start из web (совпадает с cmd/desktop).
type DesktopStartFarmRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required,min=1"`
	Mode       string  `json:"mode"`
	GameType   string  `json:"game_type"`
}

type CreateSessionRequest struct {
	Name       string  `json:"name"`
	GameType   string  `json:"game_type" binding:"required,oneof=cs2 dota2"`
	FarmMode   string  `json:"farm_mode" binding:"required,oneof=protocol sandbox"`
	AccountIDs []int64 `json:"account_ids" binding:"required,min=1"`
	Config     map[string]interface{} `json:"config"`
}

func (rt *Router) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": "0.1.0",
	})
}

func (rt *Router) createSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var id int64
	err := rt.db.Pool.QueryRow(c.Request.Context(),
		`INSERT INTO farm_sessions (name, game_type, farm_mode, account_ids, config)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		nilIfEmpty(req.Name), req.GameType, req.FarmMode, req.AccountIDs, "{}",
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, accID := range req.AccountIDs {
		_, _ = rt.db.Pool.Exec(c.Request.Context(),
			`UPDATE accounts SET status = 'queued', updated_at = NOW() WHERE id = $1`, accID)
	}

	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (rt *Router) listSessions(c *gin.Context) {
	rows, err := rt.db.Pool.Query(c.Request.Context(),
		`SELECT id, name, game_type, farm_mode, account_ids, started_at, ended_at,
		        total_hours, drops_count, status, config
		 FROM farm_sessions ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var sessions []map[string]interface{}
	for rows.Next() {
		var id int64
		var name *string
		var gameType, farmMode, status string
		var accountIDs []int64
		var startedAt interface{}
		var endedAt interface{}
		var totalHours float32
		var dropsCount int
		var config interface{}

		if err := rows.Scan(&id, &name, &gameType, &farmMode, &accountIDs,
			&startedAt, &endedAt, &totalHours, &dropsCount, &status, &config); err != nil {
			continue
		}

		sessions = append(sessions, map[string]interface{}{
			"id":           id,
			"name":         name,
			"game_type":    gameType,
			"farm_mode":    farmMode,
			"account_ids":  accountIDs,
			"started_at":   startedAt,
			"ended_at":     endedAt,
			"total_hours":  totalHours,
			"drops_count":  dropsCount,
			"status":       status,
		})
	}

	c.JSON(http.StatusOK, sessions)
}

func (rt *Router) stopSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = rt.db.Pool.Exec(c.Request.Context(),
		`UPDATE farm_sessions SET status = 'stopped', ended_at = NOW() WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// startFarmDesktopCompat создаёт farm_session и ставит аккаунты в queued (как createSession).
// Запуск песочницы/ботов есть только в sfarm-desktop; здесь — чтобы UI не получал 404.
func (rt *Router) startFarmDesktopCompat(c *gin.Context) {
	var req DesktopStartFarmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()

	var gameType, farmMode string
	err := rt.db.Pool.QueryRow(ctx,
		`SELECT game_type::text, farm_mode::text FROM accounts WHERE id = $1`, req.AccountIDs[0],
	).Scan(&gameType, &farmMode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account not found"})
		return
	}
	if req.GameType != "" {
		gameType = req.GameType
	}
	if req.Mode != "" {
		farmMode = req.Mode
	}
	if gameType != "cs2" && gameType != "dota2" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid game_type"})
		return
	}
	if farmMode != "protocol" && farmMode != "sandbox" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid farm mode"})
		return
	}

	var id int64
	err = rt.db.Pool.QueryRow(ctx,
		`INSERT INTO farm_sessions (name, game_type, farm_mode, account_ids, config)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		nilIfEmpty(""), gameType, farmMode, req.AccountIDs, "{}",
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	results := make([]map[string]interface{}, 0, len(req.AccountIDs))
	for _, accID := range req.AccountIDs {
		tag, err := rt.db.Pool.Exec(ctx,
			`UPDATE accounts SET status = 'queued', updated_at = NOW() WHERE id = $1`, accID)
		if err != nil {
			results = append(results, map[string]interface{}{"account_id": accID, "error": err.Error()})
			continue
		}
		if tag.RowsAffected() == 0 {
			results = append(results, map[string]interface{}{"account_id": accID, "error": "not found"})
			continue
		}
		results = append(results, map[string]interface{}{
			"account_id": accID,
			"mode":       farmMode,
			"status":     "queued",
		})
	}

	c.JSON(http.StatusOK, gin.H{"results": results, "session_id": id})
}

func (rt *Router) stopFarmDesktopCompat(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	for _, accID := range req.AccountIDs {
		_, _ = rt.db.Pool.Exec(ctx,
			`UPDATE accounts SET status='idle', status_detail='stopped', updated_at=NOW() WHERE id=$1`, accID)
		_, _ = rt.db.Pool.Exec(ctx,
			`UPDATE sandboxes SET status='stopped', updated_at=NOW() WHERE account_id=$1`, accID)
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (rt *Router) stopAllFarmDesktopCompat(c *gin.Context) {
	ctx := c.Request.Context()
	_, _ = rt.db.Pool.Exec(ctx,
		`UPDATE accounts SET status='idle', status_detail='stopped by user', updated_at=NOW() WHERE status IN ('farming','queued')`)
	_, _ = rt.db.Pool.Exec(ctx,
		`UPDATE sandboxes SET status='stopped', updated_at=NOW() WHERE status='running'`)
	c.JSON(http.StatusOK, gin.H{"status": "all stopped"})
}

func (rt *Router) farmStatus(c *gin.Context) {
	var farming, idle, errored, total int
	rt.db.Pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM accounts`).Scan(&total)
	rt.db.Pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM accounts WHERE status = 'farming'`).Scan(&farming)
	rt.db.Pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM accounts WHERE status = 'idle'`).Scan(&idle)
	rt.db.Pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM accounts WHERE status = 'error'`).Scan(&errored)

	c.JSON(http.StatusOK, gin.H{
		"total":   total,
		"farming": farming,
		"idle":    idle,
		"errored": errored,
	})
}
