package autoplay

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Manager coordinates CS2 bots across all running sandboxes.
type Manager struct {
	mu    sync.Mutex
	bots  map[int64]*CS2Bot // accountID -> bot
	gsi   *GSIServer
	ctx   context.Context
	yoloW *YoloWorkerProcess // общий TCP worker (YOLO), если запущен
	// yoloInferTrace ведёт лог каждого живого кадра → нейросеть (в UI yolo:log).
	yoloInferTrace func(display int, w, h, ndets int, err error)
	// memTelemetry — каждый тик бота в UI/WebSocket (cs2:mem): опрос памяти, ESP, маршрут.
	memTelemetry func(display int, ev map[string]interface{})
	previewOnce    sync.Map // display — sidecar yolo_preview.py уже запущен
}

func NewManager(ctx context.Context, yoloLogOut io.Writer, inferTrace func(display int, w, h, ndets int, err error), memTelemetry func(display int, ev map[string]interface{})) *Manager {
	gsi := NewGSIServer()
	gsi.Start()

	if err := EnsureGSIConfig(); err != nil {
		log.Printf("[Autoplay] GSI config warning: %v (CS2 GSI may not work)", err)
	}

	yproc, err := StartYoloWorker(yoloLogOut)
	if err != nil {
		log.Printf("[Autoplay] YOLO worker: %v", err)
	}

	return &Manager{
		bots:           make(map[int64]*CS2Bot),
		gsi:            gsi,
		ctx:            ctx,
		yoloW:          yproc,
		yoloInferTrace: inferTrace,
		memTelemetry:   memTelemetry,
	}
}

// ensureYoloPreviewSidecar поднимает OpenCV-окно на DISPLAY песочницы (видно в VNC).
func (m *Manager) ensureYoloPreviewSidecar(display int) {
	if os.Getenv("SFARM_YOLO_PREVIEW") != "1" {
		return
	}
	if _, loaded := m.previewOnce.LoadOrStore(display, true); loaded {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	root := FindRepoRoot(filepath.Dir(exe))
	if root == "" {
		if wd, werr := os.Getwd(); werr == nil {
			root = FindRepoRoot(wd)
		}
	}
	if root == "" {
		log.Printf("[Autoplay] YOLO preview: repo root not found")
		m.previewOnce.Delete(display)
		return
	}
	script := filepath.Join(root, "tools/cs2-yolo/yolo_preview.py")
	if st, err := os.Stat(script); err != nil || st.IsDir() {
		log.Printf("[Autoplay] YOLO preview: missing %s", script)
		m.previewOnce.Delete(display)
		return
	}
	py := resolveYoloPython(root)
	port := YoloPreviewTCPPort(display)
	cmd := exec.Command(py, script, "--port", strconv.Itoa(port))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=:%d", display))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("[Autoplay] YOLO preview: %v", err)
		m.previewOnce.Delete(display)
		return
	}
	log.Printf("[Autoplay] YOLO preview window DISPLAY=:%d port=%d", display, port)
	go func() {
		_ = cmd.Wait()
		m.previewOnce.Delete(display)
	}()
}

// StartBot creates and launches a CS2 bot for the given sandbox.
// display is the Xvfb display number (e.g. 100).
func (m *Manager) StartBot(accountID int64, display int, steamID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[accountID]; exists {
		return fmt.Errorf("bot for account %d already running", accountID)
	}

	if os.Getenv("SFARM_YOLO_PREVIEW") == "1" {
		m.ensureYoloPreviewSidecar(display)
		time.Sleep(480 * time.Millisecond)
	}

	var yc *YoloClient
	if m.yoloW != nil && m.yoloW.Client != nil {
		yc = m.yoloW.Client
	}
	var prev *YoloPreviewSink
	if os.Getenv("SFARM_YOLO_PREVIEW") == "1" {
		prev = NewYoloPreviewSink(display)
	}
	if yc == nil {
		log.Printf("[Autoplay] display=:%d: YOLO TCP worker не подключён — детекты/красные рамки на игре не появятся (проверьте [Autoplay] YOLO worker и tools/cs2-yolo)", display)
	}
	bot, err := NewCS2Bot(BotConfig{
		AccountID:      accountID,
		Display:        display,
		SteamID:        steamID,
		GSI:            m.gsi,
		Yolo:           yc,
		YoloInferTrace: m.yoloInferTrace,
		YoloPreview:    prev,
		MemTelemetry:   m.memTelemetry,
	})
	if err != nil {
		return fmt.Errorf("create bot: %w", err)
	}

	bot.Start(m.ctx)
	m.bots[accountID] = bot
	if p := ResolvedCS2MemConfigPath(); p != "" {
		log.Printf("[Autoplay] Bot started for account %d on display :%d (CS2 mem: %q — Linux process_vm_readv; при сбое после патча: make cs2-offsets)", accountID, display, p)
	} else {
		log.Printf("[Autoplay] Bot started for account %d on display :%d (CS2 mem: выкл — make cs2-offsets или config/cs2_memory.json)", accountID, display)
	}
	log.Printf("[Autoplay] CS2 nav telemetry JSONL: set SFARM_CS2_NAV_LOG=path or use default /tmp/sfarm_cs2_nav_disp%d.jsonl — disable with SFARM_CS2_NAV_LOG=off", display)
	return nil
}

func (m *Manager) StopBot(accountID int64) {
	m.mu.Lock()
	bot, ok := m.bots[accountID]
	if ok {
		delete(m.bots, accountID)
	}
	m.mu.Unlock()

	if ok {
		bot.Stop()
		log.Printf("[Autoplay] Bot stopped for account %d", accountID)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.bots))
	for id := range m.bots {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.StopBot(id)
	}
}

func (m *Manager) GetStatus(accountID int64) *BotStatus {
	m.mu.Lock()
	bot, ok := m.bots[accountID]
	m.mu.Unlock()

	if !ok {
		return nil
	}
	s := bot.Status()
	return &s
}

func (m *Manager) AllStatuses() []BotStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]BotStatus, 0, len(m.bots))
	for _, bot := range m.bots {
		result = append(result, bot.Status())
	}
	return result
}

func (m *Manager) Shutdown() {
	m.StopAll()
	if m.yoloW != nil {
		if m.yoloW.Client != nil {
			m.yoloW.Client.Close()
		}
		if m.yoloW.Cmd != nil && m.yoloW.Cmd.Process != nil {
			_ = m.yoloW.Cmd.Process.Kill()
		}
	}
	m.gsi.Stop()
}
