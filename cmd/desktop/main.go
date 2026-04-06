package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database"
	"github.com/faeronet/steam-farm-system/internal/engine"
	"github.com/faeronet/steam-farm-system/internal/engine/sandbox"
	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

//go:embed dist
var distFS embed.FS

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := common.LoadServerConfig()
	clientCfg := common.LoadClientConfig()

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	wsHub := ws.NewHub()
	go wsHub.Run()

	logCapture := NewLogCapture(wsHub, os.Stderr, 500)
	log.SetOutput(logCapture)
	log.SetFlags(log.Ldate | log.Ltime)

	log.Println("Running migrations...")
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	botManager := engine.NewManager()
	var sandboxMgr *sandbox.Manager
	sandboxMgr, err = sandbox.NewManager(clientCfg.MaxSandboxes)
	if err != nil {
		log.Printf("WARNING: Sandbox manager unavailable: %v", err)
		log.Println("Protocol-only mode enabled. Sandbox features disabled.")
	}

	farmCtrl := NewFarmController(ctx, db, botManager, sandboxMgr, wsHub, cfg, logCapture)

	go farmCtrl.RunMonitor(ctx)

	distContent, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Fatalf("Failed to load embedded UI: %v", err)
	}
	fileServer := http.FileServer(http.FS(distContent))

	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(wsHub, w, r)
	})

	farmCtrl.RegisterRoutes(mux)

	if sandboxMgr != nil {
		vncProxy := NewVNCProxy()
		mux.HandleFunc("/vnc/", vncProxy.Handle)

		inputRelay := NewInputRelay()
		mux.HandleFunc("/ws/input/", inputRelay.Handle)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(distContent, r.URL.Path[1:]); err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	appURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	openBrowser(appURL)

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║       ⚡ STEAM FARM SYSTEM — Desktop Client       ║")
	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Printf("║  UI:        %-37s ║\n", appURL)
	if sandboxMgr != nil {
		fmt.Printf("║  Sandbox:   Native OK, max %d instances              ║\n", clientCfg.MaxSandboxes)
	} else {
		fmt.Println("║  Sandbox:   UNAVAILABLE (sfarm-sandbox not found)  ║")
	}
	fmt.Println("║  Protocol:  Steam CM ready                        ║")
	fmt.Println("║                                                   ║")
	fmt.Println("║  Press Ctrl+C to stop all bots and quit           ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")
	fmt.Println()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Stopping all bots...")
	botManager.StopAll()
	if sandboxMgr != nil {
		log.Println("Stopping all sandboxes...")
		sandboxMgr.StopAll(ctx)
	}
	log.Println("Shutting down server...")
	cancel()
	server.Shutdown(ctx)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
