//go:build !linux

package autoplay

func pollCS2MemImpl(b *CS2Bot) {}

func pidAssociatesWithDisplay(_ int, _ int) bool { return false }
