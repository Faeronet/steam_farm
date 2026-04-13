package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (rt *Router) weeklyStats(c *gin.Context) {
	rows, err := rt.db.Pool.Query(c.Request.Context(),
		`SELECT week_start, game_type, accounts_farmed, total_drops, total_value, drop_breakdown
		 FROM weekly_stats ORDER BY week_start DESC LIMIT 12`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var stats []map[string]interface{}
	for rows.Next() {
		var weekStart interface{}
		var gameType string
		var accountsFarmed, totalDrops int
		var totalValue float32
		var breakdown interface{}

		if err := rows.Scan(&weekStart, &gameType, &accountsFarmed, &totalDrops, &totalValue, &breakdown); err != nil {
			continue
		}

		stats = append(stats, map[string]interface{}{
			"week_start":      weekStart,
			"game_type":       gameType,
			"accounts_farmed": accountsFarmed,
			"total_drops":     totalDrops,
			"total_value":     totalValue,
			"drop_breakdown":  breakdown,
		})
	}

	c.JSON(http.StatusOK, stats)
}

func (rt *Router) dashboardStats(c *gin.Context) {
	ctx := c.Request.Context()
	var totalAccounts, farmingAccounts, totalDropsWeek int
	var weeklyRevenue float32

	rt.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&totalAccounts)
	rt.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status = 'farming'`).Scan(&farmingAccounts)
	rt.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM drops WHERE dropped_at > NOW() - INTERVAL '7 days'`).Scan(&totalDropsWeek)
	rt.db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(market_price), 0) FROM drops WHERE dropped_at > NOW() - INTERVAL '7 days'`).Scan(&weeklyRevenue)

	var pendingRewards int
	rt.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM drops WHERE claimed = FALSE AND choice_options IS NOT NULL`).Scan(&pendingRewards)

	var activeSandboxes int
	rt.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM sandboxes WHERE status = 'running'`).Scan(&activeSandboxes)

	c.JSON(http.StatusOK, gin.H{
		"total_accounts":    totalAccounts,
		"farming_accounts":  farmingAccounts,
		"drops_this_week":   totalDropsWeek,
		"weekly_revenue":    weeklyRevenue,
		"pending_rewards":   pendingRewards,
		"active_sandboxes":  activeSandboxes,
	})
}

// listSandboxes — формат как у sandbox.Manager.List() / SandboxMonitor (id, name, cpu_percent, …).
func (rt *Router) listSandboxes(c *gin.Context) {
	rows, err := rt.db.Pool.Query(c.Request.Context(),
		`SELECT s.id, s.container_id, s.container_name, s.account_id, a.username, s.game_type,
		        s.status, s.cpu_usage, s.memory_mb, s.vnc_port, s.hostname, s.display
		 FROM sandboxes s JOIN accounts a ON s.account_id = a.id
		 ORDER BY s.id ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var sandboxes []map[string]interface{}
	for rows.Next() {
		var id, accountID int64
		var username string
		var containerID, containerName, hostname, display *string
		var gameType, status string
		var cpuUsage float32
		var memoryMB int
		var vncPort *int

		if err := rows.Scan(&id, &containerID, &containerName, &accountID, &username, &gameType,
			&status, &cpuUsage, &memoryMB, &vncPort, &hostname, &display); err != nil {
			continue
		}

		idStr := ""
		if containerID != nil && *containerID != "" {
			idStr = *containerID
		} else {
			idStr = strconv.FormatInt(id, 10)
		}
		name := ""
		if containerName != nil && *containerName != "" {
			name = *containerName
		} else {
			name = username
		}
		host := ""
		if hostname != nil && *hostname != "" {
			host = *hostname
		} else {
			host = username
		}
		disp := ":100"
		if display != nil && *display != "" {
			disp = *display
		}
		vp := 0
		if vncPort != nil {
			vp = *vncPort
		}

		sandboxes = append(sandboxes, map[string]interface{}{
			"id":          idStr,
			"name":        name,
			"account_id":  accountID,
			"game_type":   gameType,
			"status":      status,
			"vnc_port":    vp,
			"display":     disp,
			"cpu_percent": float64(cpuUsage),
			"memory_mb":   memoryMB,
			"hostname":    host,
		})
	}

	c.JSON(http.StatusOK, sandboxes)
}

func (rt *Router) sandboxStopDesktopCompat(c *gin.Context) {
	var req struct {
		AccountID int64 `json:"account_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	_, _ = rt.db.Pool.Exec(ctx,
		`UPDATE accounts SET status='idle', status_detail='stopped', updated_at=NOW() WHERE id=$1`, req.AccountID)
	_, _ = rt.db.Pool.Exec(ctx,
		`UPDATE sandboxes SET status='stopped', updated_at=NOW() WHERE account_id=$1`, req.AccountID)
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (rt *Router) autoplayStatusStub(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func (rt *Router) autoplayStartStub(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": false, "error": "autoplay is only available in sfarm-desktop"})
}

func (rt *Router) autoplayStopStub(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": false, "error": "autoplay is only available in sfarm-desktop"})
}

func (rt *Router) sandboxStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "endpoint ready"})
}
