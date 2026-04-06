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
	"sync"
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

type NativeInstance struct {
	ID       uint64
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	events   chan IpcEvent
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
	cmd := exec.CommandContext(childCtx, n.sandboxBin, "launch", "--config", string(configJSON))

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

	inst := &NativeInstance{
		ID:     cfg.ID,
		cmd:    cmd,
		cancel: cancel,
		events: make(chan IpcEvent, 64),
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
		close(inst.events)
	}()

	return inst, nil
}

func (n *NativeClient) Stop(id uint64) error {
	cmd := exec.Command(n.sandboxBin, "stop", "--id", strconv.FormatUint(id, 10))
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
