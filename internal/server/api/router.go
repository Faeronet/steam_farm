package api

import (
	"fmt"
	"net/http"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database"
	"github.com/gin-gonic/gin"
)

type Router struct {
	engine *gin.Engine
	db     *database.DB
	cfg    *common.ServerConfig
}

func NewRouter(db *database.DB, cfg *common.ServerConfig) *Router {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(corsMiddleware())

	r := &Router{engine: engine, db: db, cfg: cfg}
	r.setupRoutes()
	return r
}

func (r *Router) Run(port int) error {
	return r.engine.Run(fmt.Sprintf(":%d", port))
}

func (r *Router) Handler() http.Handler {
	return r.engine
}

func (r *Router) setupRoutes() {
	api := r.engine.Group("/api")
	{
		api.GET("/health", r.health)

		accounts := api.Group("/accounts")
		{
			accounts.GET("", r.listAccounts)
			accounts.POST("", r.createAccount)
			accounts.GET("/:id", r.getAccount)
			accounts.PUT("/:id", r.updateAccount)
			accounts.DELETE("/:id", r.deleteAccount)
			accounts.POST("/import", r.importAccounts)
		}

		farm := api.Group("/farm")
		{
			// Тот же контракт, что у cmd/desktop (web UI / Vite proxy → часто идёт на cmd/server).
			farm.POST("/start", r.startFarmDesktopCompat)
			farm.POST("/stop", r.stopFarmDesktopCompat)
			farm.POST("/stop-all", r.stopAllFarmDesktopCompat)

			farm.POST("/sessions", r.createSession)
			farm.GET("/sessions", r.listSessions)
			farm.POST("/sessions/:id/stop", r.stopSession)
			farm.GET("/status", r.farmStatus)
		}

		drops := api.Group("/drops")
		{
			drops.GET("", r.listDrops)
			drops.GET("/pending", r.pendingDrops)
			drops.POST("/:id/claim", r.claimDrop)
		}

		stats := api.Group("/stats")
		{
			stats.GET("/weekly", r.weeklyStats)
			stats.GET("/dashboard", r.dashboardStats)
		}

		sandbox := api.Group("/sandbox")
		{
			// Контракт как у cmd/desktop (SandboxMonitor.tsx).
			sandbox.GET("/list", r.listSandboxes)
			sandbox.POST("/stop", r.sandboxStopDesktopCompat)

			sandbox.GET("", r.listSandboxes)
			sandbox.GET("/:id/status", r.sandboxStatus)
		}

		api.GET("/autoplay/status", r.autoplayStatusStub)
		api.POST("/autoplay/start", r.autoplayStartStub)
		api.POST("/autoplay/stop", r.autoplayStopStub)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
