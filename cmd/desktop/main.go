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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/faeronet/steam-farm-system/internal/common"
	"github.com/faeronet/steam-farm-system/internal/database"
	"github.com/faeronet/steam-farm-system/internal/engine"
	"github.com/faeronet/steam-farm-system/internal/engine/cs2/autoplay"
	"github.com/faeronet/steam-farm-system/internal/engine/sandbox"
	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

//go:embed dist
var distFS embed.FS

// Сброс артефактов памяти CS2 от прошлых запусков (JSONL диагностики + каталоги дампа libclient).
func clearCS2MemLogArtifacts() {
	_ = os.Remove("/tmp/sfarm_cs2_mem_diag.jsonl")
	entries, err := os.ReadDir("/tmp")
	if err != nil {
		return
	}
	const prefix = "sfarm_cs2_module_"
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.RemoveAll(filepath.Join("/tmp", e.Name()))
		}
	}
}

// Дамп libclient в /tmp при первом init драйвера. По умолчанию вкл.; выкл: SFARM_CS2_MEM_MODULE_DUMP=0|off|false
func applyDefaultCS2MemModuleDump() {
	v, set := os.LookupEnv("SFARM_CS2_MEM_MODULE_DUMP")
	if !set {
		_ = os.Setenv("SFARM_CS2_MEM_MODULE_DUMP", "/tmp")
		return
	}
	s := strings.TrimSpace(v)
	if s == "0" || strings.EqualFold(s, "off") || strings.EqualFold(s, "false") || strings.EqualFold(s, "no") {
		_ = os.Unsetenv("SFARM_CS2_MEM_MODULE_DUMP")
	}
}

// Диагностика чтения CS2 (JSONL). По умолчанию включена в desktop; выкл: SFARM_CS2_MEM_DIAG=0|off|false
func applyDefaultCS2MemDiag() {
	v, set := os.LookupEnv("SFARM_CS2_MEM_DIAG")
	if !set {
		_ = os.Setenv("SFARM_CS2_MEM_DIAG", "/tmp/sfarm_cs2_mem_diag.jsonl")
	} else {
		s := strings.TrimSpace(v)
		if s == "0" || strings.EqualFold(s, "off") || strings.EqualFold(s, "false") || strings.EqualFold(s, "no") {
			_ = os.Unsetenv("SFARM_CS2_MEM_DIAG")
			_ = os.Unsetenv("SFARM_CS2_MEM_DIAG_MS")
			return
		}
	}
	if _, set := os.LookupEnv("SFARM_CS2_MEM_DIAG_MS"); !set {
		_ = os.Setenv("SFARM_CS2_MEM_DIAG_MS", "15.6")
	}
}

// Локальный HTTP-снимок libclient (127.0.0.1:17355). По умолчанию вкл.; выкл: SFARM_CS2_MEM_DEBUG_HTTP=0
func applyDefaultCS2MemDebugHTTP() {
	v, set := os.LookupEnv("SFARM_CS2_MEM_DEBUG_HTTP")
	if !set {
		_ = os.Setenv("SFARM_CS2_MEM_DEBUG_HTTP", "1")
	} else {
		s := strings.TrimSpace(v)
		if s == "0" || strings.EqualFold(s, "off") || strings.EqualFold(s, "false") || strings.EqualFold(s, "no") {
			_ = os.Unsetenv("SFARM_CS2_MEM_DEBUG_HTTP")
			_ = os.Unsetenv("SFARM_CS2_MEM_DEBUG_MS")
			return
		}
	}
	if _, set := os.LookupEnv("SFARM_CS2_MEM_DEBUG_MS"); !set {
		_ = os.Setenv("SFARM_CS2_MEM_DEBUG_MS", "15.6")
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clearCS2MemLogArtifacts()

	cfg := common.LoadServerConfig()
	clientCfg := common.LoadClientConfig()

	// Окно VNC с ROI и боксами; отключить: SFARM_YOLO_PREVIEW=0
	if _, ok := os.LookupEnv("SFARM_YOLO_PREVIEW"); !ok {
		_ = os.Setenv("SFARM_YOLO_PREVIEW", "1")
	}
	applyDefaultCS2MemModuleDump()
	applyDefaultCS2MemDiag()
	applyDefaultCS2MemDebugHTTP()
	if p := strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_MODULE_DUMP")); p != "" {
		log.Printf("[desktop] CS2 module dump parent → %q (off: SFARM_CS2_MEM_MODULE_DUMP=0)", p)
	}
	if p := strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_DIAG")); p != "" {
		log.Printf("[desktop] CS2 mem diag → %q (SFARM_CS2_MEM_DIAG_MS=%s); off: SFARM_CS2_MEM_DIAG=0",
			p, strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_DIAG_MS")))
	}
	if p := strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_DEBUG_HTTP")); p != "" {
		log.Printf("[desktop] CS2 mem debug HTTP %q (SFARM_CS2_MEM_DEBUG_MS=%s); off: SFARM_CS2_MEM_DEBUG_HTTP=0",
			p, strings.TrimSpace(os.Getenv("SFARM_CS2_MEM_DEBUG_MS")))
	}
	autoplay.StartCS2MemDebugHTTPServerIfConfigured()

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

	sigScanSink := NewSigScanLogSink(wsHub, 500)
	autoplay.SigScanLogFunc = sigScanSink.Emit

	farmCtrl := NewFarmController(ctx, db, botManager, sandboxMgr, wsHub, cfg, logCapture, sigScanSink)

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
