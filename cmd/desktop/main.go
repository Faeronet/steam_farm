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
	"sort"
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

	listenAddr := desktopHTTPListenAddr()
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("HTTP listen %q: %v", listenAddr, err)
	}
	tcpAddr := listener.Addr().(*net.TCPAddr)
	port := tcpAddr.Port
	appURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	openBrowser(desktopOpenBrowserURL(tcpAddr))

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║       ⚡ STEAM FARM SYSTEM — Desktop Client       ║")
	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Printf("║  UI (local): %-34s ║\n", appURL)
	for _, line := range formatNetworkURLLines(port, tcpAddr) {
		fmt.Printf("║  %-47s ║\n", truncateRunes(line, 47))
	}
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

// desktopHTTPListenAddr: по умолчанию все интерфейсы :8080 (LAN/VPN). Только локально: SFARM_HTTP_LISTEN=127.0.0.1:0
func desktopHTTPListenAddr() string {
	if v := strings.TrimSpace(os.Getenv("SFARM_HTTP_LISTEN")); v != "" {
		return v
	}
	return "0.0.0.0:8080"
}

// desktopOpenBrowserURL — URL для xdg-open: при bind на 0.0.0.0/127.0.0.1 — localhost, иначе тот же IP.
func desktopOpenBrowserURL(addr *net.TCPAddr) string {
	port := addr.Port
	if addr.IP == nil || addr.IP.IsUnspecified() || addr.IP.IsLoopback() {
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	return fmt.Sprintf("http://%s:%d", addr.IP.String(), port)
}

// ifaceSkipSet: по умолчанию не показываем tailscale0. SFARM_HTTP_IFACE_SKIP=- — не исключать ничего;
// иначе список имён интерфейсов через запятую (полная замена дефолта).
func ifaceSkipSet() map[string]bool {
	s := strings.TrimSpace(os.Getenv("SFARM_HTTP_IFACE_SKIP"))
	if s == "-" {
		return nil
	}
	if s == "" {
		return map[string]bool{"tailscale0": true}
	}
	m := make(map[string]bool)
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			m[name] = true
		}
	}
	return m
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

// formatNetworkURLLines — все IPv4 на поднятых интерфейсах (кроме loopback и skip), по одному URL на адрес.
func formatNetworkURLLines(port int, tcpAddr *net.TCPAddr) []string {
	entries := listHTTPBindURLEntries(port, tcpAddr)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries)+1)
	out = append(out, "По сети:")
	for _, e := range entries {
		out = append(out, "  "+e.String())
	}
	return out
}

type httpBindEntry struct {
	URL   string
	Iface string
}

func (e httpBindEntry) String() string {
	if e.Iface != "" {
		return fmt.Sprintf("%s (%s)", e.URL, e.Iface)
	}
	return e.URL
}

func listHTTPBindURLEntries(port int, tcpAddr *net.TCPAddr) []httpBindEntry {
	if tcpAddr != nil && tcpAddr.IP != nil && tcpAddr.IP.IsLoopback() {
		return nil
	}
	if tcpAddr != nil && tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() && !tcpAddr.IP.IsLoopback() {
		return []httpBindEntry{{URL: fmt.Sprintf("http://%s:%d", tcpAddr.IP.String(), port)}}
	}
	return enumerateInterfaceURLEntries(port, ifaceSkipSet())
}

func enumerateInterfaceURLEntries(port int, skip map[string]bool) []httpBindEntry {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	type pair struct {
		ip   string
		name string
	}
	var pairs []pair
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if (iface.Flags & net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		if skip != nil && skip[iface.Name] {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			pairs = append(pairs, pair{ip: s, name: iface.Name})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].ip < pairs[j].ip
	})
	out := make([]httpBindEntry, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, httpBindEntry{
			URL:   fmt.Sprintf("http://%s:%d", p.ip, port),
			Iface: p.name,
		})
	}
	return out
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
