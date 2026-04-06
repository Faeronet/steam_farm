package sandbox

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

type DisplayConfig struct {
	Display    string
	Width      int
	Height     int
	VNCPort    int
}

func DefaultDisplay(vncPort int) DisplayConfig {
	return DisplayConfig{
		Display: ":99",
		Width:   640,
		Height:  480,
		VNCPort: vncPort,
	}
}

func StartXvfb(ctx context.Context, cfg DisplayConfig) error {
	args := []string{
		cfg.Display,
		"-screen", "0", fmt.Sprintf("%dx%dx24", cfg.Width, cfg.Height),
		"-ac",
	}

	cmd := exec.CommandContext(ctx, "Xvfb", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Xvfb: %w", err)
	}

	log.Printf("[Display] Xvfb started on %s (%dx%d)", cfg.Display, cfg.Width, cfg.Height)
	return nil
}

func StartVNCServer(ctx context.Context, display string, port int) error {
	cmd := exec.CommandContext(ctx, "x11vnc",
		"-display", display,
		"-rfbport", fmt.Sprintf("%d", port),
		"-nopw",
		"-forever",
		"-shared",
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start VNC: %w", err)
	}

	log.Printf("[Display] VNC server started on port %d", port)
	return nil
}
