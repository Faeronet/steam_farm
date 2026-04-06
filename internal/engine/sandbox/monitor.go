package sandbox

import (
	"context"
	"log"
	"time"
)

type ResourceSnapshot struct {
	ContainerID string  `json:"container_id"`
	AccountID   int64   `json:"account_id"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryMB    int     `json:"memory_mb"`
	Running     bool    `json:"running"`
}

type Monitor struct {
	manager  *Manager
	interval time.Duration
	onChange func([]ResourceSnapshot)
}

func NewMonitor(manager *Manager, interval time.Duration) *Monitor {
	return &Monitor{
		manager:  manager,
		interval: interval,
	}
}

func (m *Monitor) SetOnChange(fn func([]ResourceSnapshot)) {
	m.onChange = fn
}

func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshots := m.collectStats(ctx)
			if m.onChange != nil && len(snapshots) > 0 {
				m.onChange(snapshots)
			}
		}
	}
}

func (m *Monitor) collectStats(ctx context.Context) []ResourceSnapshot {
	containers := m.manager.List()
	snapshots := make([]ResourceSnapshot, 0, len(containers))

	for _, c := range containers {
		stats, err := m.manager.docker.Stats(ctx, c.ID)
		if err != nil {
			log.Printf("[Monitor] Stats error for %s: %v", c.Name, err)
			continue
		}

		snapshots = append(snapshots, ResourceSnapshot{
			ContainerID: c.ID,
			AccountID:   c.AccountID,
			CPUPercent:  stats.CPUPercent,
			MemoryMB:    stats.MemoryMB,
			Running:     stats.Running,
		})
	}

	return snapshots
}
