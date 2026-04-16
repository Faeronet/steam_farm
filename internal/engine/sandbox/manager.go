package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/faeronet/steam-farm-system/internal/engine/cs2/autoplay"
)

type ContainerStatus string

const (
	ContainerStopped  ContainerStatus = "stopped"
	ContainerStarting ContainerStatus = "starting"
	ContainerRunning  ContainerStatus = "running"
	ContainerError    ContainerStatus = "error"
)

type ContainerInfo struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	AccountID  int64           `json:"account_id"`
	GameType   string          `json:"game_type"`
	Status     ContainerStatus `json:"status"`
	MachineID  string          `json:"machine_id"`
	MACAddress string          `json:"mac_address"`
	Hostname   string          `json:"hostname"`
	VNCPort    int             `json:"vnc_port"`
	CPUPercent float64         `json:"cpu_percent"`
	MemoryMB   int             `json:"memory_mb"`
	Display    string          `json:"display"`
	CS2PID     int             `json:"cs2_pid,omitempty"`
}

type Manager struct {
	mu         sync.RWMutex
	containers map[int64]*ContainerInfo
	instances  map[int64]*NativeInstance
	native     *NativeClient
	maxSlots   int
}

func NewManager(maxSlots int) (*Manager, error) {
	nc, err := NewNativeClient()
	if err != nil {
		return nil, fmt.Errorf("native sandbox client: %w", err)
	}

	return &Manager{
		containers: make(map[int64]*ContainerInfo),
		instances:  make(map[int64]*NativeInstance),
		native:     nc,
		maxSlots:   maxSlots,
	}, nil
}

func (m *Manager) Launch(ctx context.Context, accountID int64, gameType string, username string, password string) (*ContainerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.containers) >= m.maxSlots {
		return nil, fmt.Errorf("max sandbox slots reached (%d)", m.maxSlots)
	}

	if _, exists := m.containers[accountID]; exists {
		return nil, fmt.Errorf("sandbox for account %d already exists", accountID)
	}

	machineID := GenerateMachineID()
	hostname := fmt.Sprintf("farm-bot-%d", accountID)
	slotIndex := len(m.containers)
	vncPort := 5900 + slotIndex
	displayNum := 100 + slotIndex

	tmpl := GetTemplate(gameType)
	launchOpts := ""
	if tmpl != nil {
		launchOpts = tmpl.LaunchOpts
	}

	cfg := SandboxConfig{
		ID:         uint64(accountID),
		Game:       gameType,
		Username:   username,
		Password:   password,
		VNCPort:    uint16(vncPort),
		Display:    uint16(displayNum),
		LaunchOpts: launchOpts,
	}

	instance, err := m.native.Launch(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("launch sandbox: %w", err)
	}

	info := &ContainerInfo{
		ID:        fmt.Sprintf("sandbox-%d", accountID),
		Name:      fmt.Sprintf("sfarm-%s-%d", gameType, accountID),
		AccountID: accountID,
		GameType:  gameType,
		Status:    ContainerRunning,
		MachineID: machineID,
		Hostname:  hostname,
		VNCPort:   vncPort,
		Display:   fmt.Sprintf(":%d", displayNum),
	}

	m.containers[accountID] = info
	m.instances[accountID] = instance

	go m.watchInstance(accountID, instance)

	log.Printf("[Sandbox] Launched %s for account %d (vnc: %d, display: :%d)", gameType, accountID, vncPort, displayNum)
	return info, nil
}

func (m *Manager) watchInstance(accountID int64, inst *NativeInstance) {
	for ev := range inst.Events() {
		switch ev.Event {
		case "stats":
			m.mu.Lock()
			if info, ok := m.containers[accountID]; ok {
				info.CPUPercent = ev.CPU
				info.MemoryMB = int(ev.MemoryMB)
			}
			m.mu.Unlock()
		case "cs2_pid":
			if ev.PID == 0 {
				autoplay.ClearSandboxReportedCS2PID(accountID)
				m.mu.Lock()
				if info, ok := m.containers[accountID]; ok {
					info.CS2PID = 0
				}
				m.mu.Unlock()
			} else if ev.PID > 0 {
				autoplay.SetSandboxReportedCS2PID(accountID, int(ev.PID))
				m.mu.Lock()
				if info, ok := m.containers[accountID]; ok {
					info.CS2PID = int(ev.PID)
				}
				m.mu.Unlock()
			}
		case "exited":
			autoplay.ClearSandboxReportedCS2PID(accountID)
			m.mu.Lock()
			if info, ok := m.containers[accountID]; ok {
				info.Status = ContainerStopped
				info.CS2PID = 0
			}
			delete(m.instances, accountID)
			m.mu.Unlock()
			log.Printf("[Sandbox] Instance for account %d exited (code=%d)", accountID, ev.Code)
		case "error":
			m.mu.Lock()
			if info, ok := m.containers[accountID]; ok {
				info.Status = ContainerError
			}
			m.mu.Unlock()
			log.Printf("[Sandbox] Error for account %d: %s", accountID, ev.Message)
		}
	}
}

func (m *Manager) Stop(ctx context.Context, accountID int64) error {
	m.mu.Lock()
	inst, hasInst := m.instances[accountID]
	_, hasCont := m.containers[accountID]
	m.mu.Unlock()

	if !hasCont {
		return fmt.Errorf("no sandbox for account %d", accountID)
	}

	// 1) SIGTERM the Rust supervisor first so it can run shutdown() (kill Steam/CS2/Xvfb).
	// 2) Never call cancel() before that — context cancel sends SIGKILL and skips Rust cleanup (orphan game).
	if err := m.native.Stop(uint64(accountID)); err != nil {
		log.Printf("[Sandbox] Stop signal for %d: %v", accountID, err)
	}

	if hasInst && inst.exited != nil {
		select {
		case <-inst.exited:
			log.Printf("[Sandbox] Supervisor for account %d exited", accountID)
		case <-time.After(40 * time.Second):
			log.Printf("[Sandbox] Account %d supervisor still running after SIGTERM — forcing cancel", accountID)
			inst.cancel()
			select {
			case <-inst.exited:
			case <-time.After(5 * time.Second):
			}
		}
	}

	m.mu.Lock()
	delete(m.containers, accountID)
	delete(m.instances, accountID)
	m.mu.Unlock()
	autoplay.ClearSandboxReportedCS2PID(accountID)

	return nil
}

func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.containers))
	for id := range m.containers {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Stop(ctx, id)
	}
}

func (m *Manager) List() []ContainerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ContainerInfo, 0, len(m.containers))
	for _, info := range m.containers {
		result = append(result, *info)
	}
	return result
}

func (m *Manager) Get(accountID int64) (*ContainerInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, exists := m.containers[accountID]
	return info, exists
}

func (m *Manager) GetStats(accountID int64) *ContainerStats {
	m.mu.RLock()
	inst, ok := m.instances[accountID]
	m.mu.RUnlock()

	if !ok {
		return &ContainerStats{Running: false, Status: "stopped"}
	}
	return inst.Stats()
}

func findSteamPaths() (appsCommon string, steamRoot string) {
	home, _ := os.UserHomeDir()
	roots := []string{
		filepath.Join(home, "snap/steam/common/.local/share/Steam"),
		filepath.Join(home, ".local/share/Steam"),
		filepath.Join(home, ".steam/steam"),
		filepath.Join(home, ".steam/debian-installation"),
	}
	for _, root := range roots {
		common := filepath.Join(root, "steamapps/common")
		if info, err := os.Stat(common); err == nil && info.IsDir() {
			return common, root
		}
	}
	return "", ""
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.containers)
}
