package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

// YoloLogSink буферизует stdout/stderr yolo_worker построчно и шлёт в WebSocket + кольцо для /api/yolo-logs.
type YoloLogSink struct {
	mu      sync.Mutex
	lineBuf bytes.Buffer
	ring    []LogEntry
	maxSize int
	hub     *ws.Hub
	tee     io.Writer
}

func NewYoloLogSink(hub *ws.Hub, tee io.Writer, maxSize int) *YoloLogSink {
	if maxSize <= 0 {
		maxSize = 300
	}
	return &YoloLogSink{
		ring:    make([]LogEntry, 0, 32),
		maxSize: maxSize,
		hub:     hub,
		tee:     tee,
	}
}

func (s *YoloLogSink) Write(p []byte) (n int, err error) {
	if s.tee != nil {
		_, _ = s.tee.Write(p)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lineBuf.Write(p)
	data := s.lineBuf.Bytes()
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(data[:idx]))
		s.lineBuf.Next(idx + 1)
		data = s.lineBuf.Bytes()
		if line != "" {
			s.pushLineUnlocked(line)
		}
	}
	return len(p), nil
}

func (s *YoloLogSink) pushLineUnlocked(line string) {
	level := yoloLogLevel(line)
	entry := LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Level:   level,
		Message: line,
	}
	if len(s.ring) >= s.maxSize {
		s.ring = s.ring[1:]
	}
	s.ring = append(s.ring, entry)
	if s.hub != nil {
		go s.hub.Broadcast(ws.EventYoloLog, entry)
	}
}

func yoloLogLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "traceback") || strings.Contains(lower, "exception"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warning"
	case strings.Contains(lower, "idle ping"):
		return "success"
	default:
		return "info"
	}
}

func (s *YoloLogSink) Recent(n int) []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.ring) {
		n = len(s.ring)
	}
	start := len(s.ring) - n
	out := make([]LogEntry, n)
	copy(out, s.ring[start:])
	return out
}
