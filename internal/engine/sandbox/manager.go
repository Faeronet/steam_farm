package sandbox

import (
	"context"
	"fmt"
	"log"
	"sync"
)

type ContainerStatus string

const (
	ContainerStopped  ContainerStatus = "stopped"
	ContainerStarting ContainerStatus = "starting"
	ContainerRunning  ContainerStatus = "running"
	ContainerError    ContainerStatus = "error"
)

type ContainerInfo struct {
	ID            string
	Name          string
	AccountID     int64
	GameType      string
	Status        ContainerStatus
	MachineID     string
	MACAddress    string
	Hostname      string
	VNCPort       int
	CPUPercent    float64
	MemoryMB      int
	Display       string
}

type Manager struct {
	mu         sync.RWMutex
	containers map[int64]*ContainerInfo
	docker     *DockerClient
	maxSlots   int
}

func NewManager(maxSlots int) (*Manager, error) {
	dc, err := NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	return &Manager{
		containers: make(map[int64]*ContainerInfo),
		docker:     dc,
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
	vncPort := 5900 + len(m.containers)

	config := ContainerConfig{
		Name:      fmt.Sprintf("sfarm-%s-%d", gameType, accountID),
		GameType:  gameType,
		MachineID: machineID,
		Hostname:  hostname,
		VNCPort:   vncPort,
		Display:   ":99",
		Username:  username,
		Password:  password,
	}

	containerID, err := m.docker.CreateAndStart(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("launch container: %w", err)
	}

	info := &ContainerInfo{
		ID:        containerID,
		Name:      config.Name,
		AccountID: accountID,
		GameType:  gameType,
		Status:    ContainerRunning,
		MachineID: machineID,
		Hostname:  hostname,
		VNCPort:   vncPort,
		Display:   ":99",
	}

	m.containers[accountID] = info
	log.Printf("[Sandbox] Launched %s for account %d (container: %s)", gameType, accountID, containerID[:12])

	return info, nil
}

func (m *Manager) Stop(ctx context.Context, accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.containers[accountID]
	if !exists {
		return fmt.Errorf("no sandbox for account %d", accountID)
	}

	if err := m.docker.Stop(ctx, info.ID); err != nil {
		return err
	}

	if err := m.docker.Remove(ctx, info.ID); err != nil {
		log.Printf("[Sandbox] Failed to remove container %s: %v", info.ID[:12], err)
	}

	delete(m.containers, accountID)
	return nil
}

func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for accountID, info := range m.containers {
		_ = m.docker.Stop(ctx, info.ID)
		_ = m.docker.Remove(ctx, info.ID)
		delete(m.containers, accountID)
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

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.containers)
}
