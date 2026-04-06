package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database"
	"github.com/faeronet/steam-farm-system/internal/server/api"
	grpcservice "github.com/faeronet/steam-farm-system/internal/server/grpc"
	"github.com/faeronet/steam-farm-system/internal/server/telegram"
	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := common.LoadServerConfig()

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Running database migrations...")
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Migrations completed")

	wsHub := ws.NewHub()
	go wsHub.Run()

	farmService := grpcservice.NewFarmService(db, wsHub)
	tgBot := telegram.NewBot(cfg.TelegramToken, cfg.TelegramChatID)

	router := api.NewRouter(db, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(wsHub, w, r)
	})
	mux.HandleFunc("/ws/worker", farmService.HandleWorkerConnect)
	mux.Handle("/", router.Handler())

	if tgBot.IsConfigured() {
		log.Println("Telegram bot configured")
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP server starting on :%d", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	cancel()
	server.Shutdown(ctx)
}
