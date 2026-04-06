package api

import (
	"net/http"

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

func (rt *Router) listSandboxes(c *gin.Context) {
	rows, err := rt.db.Pool.Query(c.Request.Context(),
		`SELECT s.id, s.account_id, a.username, s.container_name, s.game_type,
		        s.status, s.cpu_usage, s.memory_mb, s.vnc_port
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
		var containerName *string
		var gameType, status string
		var cpuUsage float32
		var memoryMB int
		var vncPort *int

		if err := rows.Scan(&id, &accountID, &username, &containerName, &gameType,
			&status, &cpuUsage, &memoryMB, &vncPort); err != nil {
			continue
		}

		sandboxes = append(sandboxes, map[string]interface{}{
			"id":             id,
			"account_id":     accountID,
			"username":       username,
			"container_name": containerName,
			"game_type":      gameType,
			"status":         status,
			"cpu_usage":      cpuUsage,
			"memory_mb":      memoryMB,
			"vnc_port":       vncPort,
		})
	}

	c.JSON(http.StatusOK, sandboxes)
}

func (rt *Router) sandboxStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "endpoint ready"})
}
