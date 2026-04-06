package main

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type LogCapture struct {
	mu      sync.Mutex
	buf     []LogEntry
	maxSize int
	hub     *ws.Hub
	orig    io.Writer
}

func NewLogCapture(hub *ws.Hub, orig io.Writer, maxSize int) *LogCapture {
	return &LogCapture{
		buf:     make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
		hub:     hub,
		orig:    orig,
	}
}

func (lc *LogCapture) Write(p []byte) (n int, err error) {
	if lc.orig != nil {
		lc.orig.Write(p)
	}

	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}

	cleaned := msg
	if len(cleaned) > 20 && cleaned[4] == '/' && cleaned[7] == '/' {
		if idx := strings.Index(cleaned, " "); idx > 0 {
			if idx2 := strings.Index(cleaned[idx+1:], " "); idx2 > 0 {
				cleaned = cleaned[idx+1+idx2+1:]
			}
		}
	}

	level := "info"
	lower := strings.ToLower(cleaned)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		level = "error"
	case strings.Contains(lower, "warn"):
		level = "warning"
	case strings.Contains(lower, "logged in") || strings.Contains(lower, "started") || strings.Contains(lower, "success"):
		level = "success"
	}

	msg = cleaned

	entry := LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Level:   level,
		Message: msg,
	}

	lc.mu.Lock()
	if len(lc.buf) >= lc.maxSize {
		lc.buf = lc.buf[1:]
	}
	lc.buf = append(lc.buf, entry)
	lc.mu.Unlock()

	if lc.hub != nil {
		go lc.hub.Broadcast(ws.EventLog, entry)
	}

	return len(p), nil
}

func (lc *LogCapture) Recent(n int) []LogEntry {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if n <= 0 || n > len(lc.buf) {
		n = len(lc.buf)
	}
	start := len(lc.buf) - n
	result := make([]LogEntry, n)
	copy(result, lc.buf[start:])
	return result
}
