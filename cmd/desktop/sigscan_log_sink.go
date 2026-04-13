package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/faeronet/steam-farm-system/internal/server/ws"
)

// SigScanLogSink collects sigscanner log lines for the web panel console and writes to a separate log file.
type SigScanLogSink struct {
	mu      sync.Mutex
	ring    []LogEntry
	maxSize int
	hub     *ws.Hub
	file    *os.File
}

const defaultSigScanLogPath = "/tmp/sfarm_sigscan.log"

func NewSigScanLogSink(hub *ws.Hub, maxSize int) *SigScanLogSink {
	if maxSize <= 0 {
		maxSize = 500
	}
	s := &SigScanLogSink{
		ring:    make([]LogEntry, 0, 32),
		maxSize: maxSize,
		hub:     hub,
	}
	p := os.Getenv("SFARM_SIGSCAN_LOG")
	if p == "" {
		p = defaultSigScanLogPath
	}
	if p != "0" && p != "off" && p != "false" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SigScanLog] open %q: %v\n", p, err)
		} else {
			s.file = f
		}
	}
	return s
}

func (s *SigScanLogSink) Emit(level, msg string) {
	now := time.Now()
	entry := LogEntry{
		Time:    now.Format("15:04:05"),
		Level:   level,
		Message: msg,
	}
	s.mu.Lock()
	if len(s.ring) >= s.maxSize {
		s.ring = s.ring[1:]
	}
	s.ring = append(s.ring, entry)
	s.mu.Unlock()

	if s.file != nil {
		line := fmt.Sprintf("%s [%s] %s\n", now.Format("2006/01/02 15:04:05.000"), level, msg)
		_, _ = s.file.Write([]byte(line))
	}
	if s.hub != nil {
		go s.hub.Broadcast(ws.EventSigScanLog, entry)
	}
}

func (s *SigScanLogSink) Recent(n int) []LogEntry {
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

func (s *SigScanLogSink) Close() {
	if s.file != nil {
		_ = s.file.Close()
	}
}
