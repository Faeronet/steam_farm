package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (rt *Router) listDrops(c *gin.Context) {
	gameType := c.Query("game_type")
	limit := c.DefaultQuery("limit", "100")

	query := `SELECT id, account_id, session_id, game_type, item_name, item_type,
	           item_image_url, market_price, dropped_at, claimed, sent_to_trade,
	           choice_options, chosen_items
	          FROM drops WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if gameType != "" {
		query += ` AND game_type = $` + strconv.Itoa(argIdx)
		args = append(args, gameType)
		argIdx++
	}

	query += ` ORDER BY dropped_at DESC LIMIT $` + strconv.Itoa(argIdx)
	l, _ := strconv.Atoi(limit)
	if l <= 0 || l > 500 {
		l = 100
	}
	args = append(args, l)

	rows, err := rt.db.Pool.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			"id":             id,
			"account_id":     accountID,
			"session_id":     sessionID,
			"game_type":      gameType,
			"item_name":      itemName,
			"item_type":      itemType,
			"item_image_url": imageURL,
			"market_price":   price,
			"dropped_at":     droppedAt,
			"claimed":        claimed,
			"sent_to_trade":  sentToTrade,
			"choice_options": choiceOpts,
			"chosen_items":   chosenItems,
		})
	}

	c.JSON(http.StatusOK, drops)
}

func (rt *Router) pendingDrops(c *gin.Context) {
	rows, err := rt.db.Pool.Query(c.Request.Context(),
		`SELECT d.id, d.account_id, a.username, d.game_type, d.item_name, d.choice_options
		 FROM drops d JOIN accounts a ON d.account_id = a.id
		 WHERE d.claimed = FALSE AND d.choice_options IS NOT NULL
		 ORDER BY d.dropped_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var pending []map[string]interface{}
	for rows.Next() {
		var id, accountID int64
		var username, gameType, itemName string
		var choiceOpts interface{}

		if err := rows.Scan(&id, &accountID, &username, &gameType, &itemName, &choiceOpts); err != nil {
			continue
		}

		pending = append(pending, map[string]interface{}{
			"id":             id,
			"account_id":     accountID,
			"username":       username,
			"game_type":      gameType,
			"item_name":      itemName,
			"choice_options": choiceOpts,
		})
	}

	c.JSON(http.StatusOK, pending)
}

func (rt *Router) claimDrop(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req struct {
		ChosenItems []string `json:"chosen_items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = rt.db.Pool.Exec(c.Request.Context(),
		`UPDATE drops SET claimed = TRUE, chosen_items = $1 WHERE id = $2`,
		req.ChosenItems, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "claimed"})
}
