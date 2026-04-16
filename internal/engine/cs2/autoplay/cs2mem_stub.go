//go:build !linux

package autoplay

func pollCS2MemImpl(b *CS2Bot) {}

func pidAssociatesWithDisplay(_ int, _ int) bool { return false }

func pidBelongsToSandboxAccount(_ int, _ int64) bool { return false }
