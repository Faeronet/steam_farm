package api

import (
	"net/http"
	"strconv"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database/models"
	"github.com/gin-gonic/gin"
)

type CreateAccountRequest struct {
	Username       string `json:"username" binding:"required"`
	Password       string `json:"password" binding:"required"`
	SharedSecret   string `json:"shared_secret"`
	IdentitySecret string `json:"identity_secret"`
	GameType       string `json:"game_type" binding:"required,oneof=cs2 dota2"`
	FarmMode       string `json:"farm_mode" binding:"oneof=protocol sandbox"`
	Proxy          string `json:"proxy"`
	GroupName      string `json:"group_name"`
}

func (rt *Router) listAccounts(c *gin.Context) {
	gameType := c.Query("game_type")
	status := c.Query("status")

	query := `SELECT * FROM accounts WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if gameType != "" {
		query += ` AND game_type = $` + strconv.Itoa(argIdx)
		args = append(args, gameType)
		argIdx++
	}
	if status != "" {
		query += ` AND status = $` + strconv.Itoa(argIdx)
		args = append(args, status)
		argIdx++
	}
	query += ` ORDER BY id ASC`

	rows, err := rt.db.Pool.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		err := rows.Scan(
			&a.ID, &a.Username, &a.PasswordEnc, &a.SharedSecret, &a.IdentitySecret,
			&a.SteamID, &a.AvatarURL, &a.PersonaName, &a.Proxy, &a.GameType,
			&a.FarmMode, &a.Status, &a.StatusDetail, &a.IsPrime, &a.CS2Level,
			&a.CS2XP, &a.CS2XPNeeded, &a.CS2Rank, &a.ArmoryStars, &a.DotaHours,
			&a.LastDropAt, &a.FarmedThisWeek, &a.DropCollected, &a.Tags, &a.GroupName,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		accounts = append(accounts, a)
	}

	c.JSON(http.StatusOK, accounts)
}

func (rt *Router) createAccount(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.FarmMode == "" {
		req.FarmMode = "sandbox"
	}

	encrypted, err := common.Encrypt([]byte(req.Password), rt.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return
	}

	var id int64
	err = rt.db.Pool.QueryRow(c.Request.Context(),
		`INSERT INTO accounts (username, password_enc, shared_secret, identity_secret, game_type, farm_mode, proxy, group_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		req.Username, encrypted, nilIfEmpty(req.SharedSecret), nilIfEmpty(req.IdentitySecret),
		req.GameType, req.FarmMode, nilIfEmpty(req.Proxy), nilIfEmpty(req.GroupName),
	).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (rt *Router) getAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var a models.Account
	err = rt.db.Pool.QueryRow(c.Request.Context(),
		`SELECT * FROM accounts WHERE id = $1`, id,
	).Scan(
		&a.ID, &a.Username, &a.PasswordEnc, &a.SharedSecret, &a.IdentitySecret,
		&a.SteamID, &a.AvatarURL, &a.PersonaName, &a.Proxy, &a.GameType,
		&a.FarmMode, &a.Status, &a.StatusDetail, &a.IsPrime, &a.CS2Level,
		&a.CS2XP, &a.CS2XPNeeded, &a.CS2Rank, &a.ArmoryStars, &a.DotaHours,
		&a.LastDropAt, &a.FarmedThisWeek, &a.DropCollected, &a.Tags, &a.GroupName,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	c.JSON(http.StatusOK, a)
}

func (rt *Router) updateAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = rt.db.Pool.Exec(c.Request.Context(),
		`UPDATE accounts SET updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (rt *Router) deleteAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = rt.db.Pool.Exec(c.Request.Context(),
		`DELETE FROM accounts WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (rt *Router) importAccounts(c *gin.Context) {
	// TODO: bulk import from CSV/maFile
	c.JSON(http.StatusOK, gin.H{"status": "import endpoint ready"})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
