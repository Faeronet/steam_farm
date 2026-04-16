package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"os/user"
)

type ContainerStats struct {
	Running    bool
	Status     string
	CPUPercent float64
	MemoryMB   int
}

type NativeClient struct {
	sandboxBin string
}

type SandboxConfig struct {
	ID         uint64 `json:"id"`
	Game       string `json:"game"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	VNCPort    uint16 `json:"vnc_port"`
	Display    uint16 `json:"display"`
	LaunchOpts string `json:"launch_opts,omitempty"`
}

type IpcEvent struct {
	Event    string  `json:"event"`
	PID      uint32  `json:"pid,omitempty"`
	VNCPort  uint16  `json:"vnc_port,omitempty"`
	AppID    uint32  `json:"app_id,omitempty"`
	CPU      float64 `json:"cpu,omitempty"`
	MemoryMB uint64  `json:"memory_mb,omitempty"`
	Code     int     `json:"code,omitempty"`
	Message  string  `json:"message,omitempty"`
}

const waitSandboxStarted = 3 * time.Minute

// runuserPath: snap Steam из Ubuntu Software привязан к обычному пользователю; `snap run steam` от root
// часто не поднимает клиент. При euid=0 запускаем sfarm-sandbox через runuser(1) от steam-farm (или SFARM_SANDBOX_USER).
func runuserPath() string {
	for _, p := range []string{"/usr/sbin/runuser", "/sbin/runuser"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	p, err := exec.LookPath("runuser")
	if err == nil {
		return p
	}
	return ""
}

// sandboxInvokerUser: если непустой — запускать `runuser -u name -- sfarm-sandbox ...` (только от euid=0).
func sandboxInvokerUser() (string, bool) {
	if os.Geteuid() != 0 {
		return "", false
	}
	if v := strings.TrimSpace(os.Getenv("SFARM_SANDBOX_USER")); v != "" {
		return v, true
	}
	if _, err := user.Lookup("steam-farm"); err == nil {
		return "steam-farm", true
	}
	return "", false
}

func (n *NativeClient) commandSandbox(ctx context.Context, sandboxArgs ...string) *exec.Cmd {
	u, use := sandboxInvokerUser()
	runuser := runuserPath()
	if use && runuser != "" {
		log.Printf("[Sandbox] sfarm-sandbox via runuser -u %s (snap Steam из магазина — не от root); binary=%s", u, n.sandboxBin)
		args := append([]string{"-u", u, "--", n.sandboxBin}, sandboxArgs...)
		return exec.CommandContext(ctx, runuser, args...)
	}
	if use && runuser == "" {
		log.Printf("[Sandbox] runuser not found — running sfarm-sandbox as root (snap Steam may fail); install util-linux")
	}
	return exec.CommandContext(ctx, n.sandboxBin, sandboxArgs...)
}

type NativeInstance struct {
	ID       uint64
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	events   chan IpcEvent
	exited   chan struct{} // closed after sfarm-sandbox launch process exits (Wait returns)
	mu       sync.Mutex
	lastStat *IpcEvent
}

func NewNativeClient() (*NativeClient, error) {
	binPath, err := findSandboxBinary()
	if err != nil {
		return nil, err
	}
	return &NativeClient{sandboxBin: binPath}, nil
}

func findSandboxBinary() (string, error) {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(exeDir, "sfarm-sandbox"),
		filepath.Join(exeDir, "..", "sandbox-core", "target", "release", "sfarm-sandbox"),
		"sandbox-core/target/release/sfarm-sandbox",
		"bin/sfarm-sandbox",
		"sfarm-sandbox",
	}

	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, nil
		}
	}

	if p, err := exec.LookPath("sfarm-sandbox"); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("sfarm-sandbox binary not found; build with: cd sandbox-core && cargo build --release")
}

func (n *NativeClient) Launch(ctx context.Context, cfg SandboxConfig) (*NativeInstance, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	childCtx, cancel := context.WithCancel(ctx)
	cmd := n.commandSandbox(childCtx, "launch", "--config", string(configJSON))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start sandbox: %w", err)
	}

	exitedCh := make(chan struct{})
	inst := &NativeInstance{
		ID:     cfg.ID,
		cmd:    cmd,
		cancel: cancel,
		events: make(chan IpcEvent, 64),
		exited: exitedCh,
	}

	// Read IPC JSON events from stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var ev IpcEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				log.Printf("[Sandbox-%d] Bad IPC line: %s", cfg.ID, string(line))
				continue
			}

			if ev.Event == "stats" {
				inst.mu.Lock()
				evCopy := ev
				inst.lastStat = &evCopy
				inst.mu.Unlock()
			} else {
				log.Printf("[Sandbox-%d] Event: %s", cfg.ID, ev.Event)
			}

			select {
			case inst.events <- ev:
			default:
			}
		}
	}()

	// Forward sandbox stderr to Go logger (visible in web UI)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			log.Printf("[Sandbox-%d] %s", cfg.ID, scanner.Text())
		}
	}()

	go func() {
		_ = cmd.Wait()
		close(exitedCh)
		close(inst.events)
	}()

	// Не возвращаемся, пока Xvfb+x11vnc не готовы (IPC started) — иначе автоплей/X11Input
	// стартует раньше сокета :N и минутами крутит «Display not ready».
	select {
	case ev, ok := <-inst.events:
		if !ok {
			cancel()
			return nil, fmt.Errorf("sandbox process exited before X11 was ready")
		}
		switch ev.Event {
		case "started":
			// ok
		case "error":
			cancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil, fmt.Errorf("sandbox: %s", ev.Message)
		default:
			cancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil, fmt.Errorf("sandbox: expected started, got %q", ev.Event)
		}
	case <-time.After(waitSandboxStarted):
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("timeout waiting for sandbox started (X11/VNC); check sfarm-sandbox logs and Xvfb")
	}

	return inst, nil
}

func (n *NativeClient) Stop(id uint64) error {
	cmd := n.commandSandbox(context.Background(), "stop", "--id", strconv.FormatUint(id, 10))
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (inst *NativeInstance) Stats() *ContainerStats {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.lastStat == nil {
		return &ContainerStats{Running: true, Status: "running"}
	}
	return &ContainerStats{
		Running:    true,
		Status:     "running",
		CPUPercent: inst.lastStat.CPU,
		MemoryMB:   int(inst.lastStat.MemoryMB),
	}
}

func (inst *NativeInstance) Kill() {
	inst.cancel()
}

func (inst *NativeInstance) Events() <-chan IpcEvent {
	return inst.events
}
