//go:build linux

package autoplay

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const steamDialogDismissCooldown = 8 * time.Second

// maybeDismissSteamDialogs закрывает типичные Steam-уведомления (Remote Play и т.д.).
func maybeDismissSteamDialogs(display int, lastRun *time.Time) {
	dismissSteamLikeDialogs(display, lastRun, false)
}

// dismissSteamLikeDialogs — Remote Play + отдельные X-окна с «OK» / disconnect (xdotool).
// force=true игнорирует cooldown (при повторной постановке после кика).
func dismissSteamLikeDialogs(display int, lastRun *time.Time, force bool) {
	if !force && lastRun != nil && time.Since(*lastRun) < steamDialogDismissCooldown {
		return
	}
	if strings.TrimSpace(os.Getenv("SFARM_STEAM_XDOTOOL")) == "0" {
		return
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return
	}

	disp := steamDisplayEnv(display)
	env := append(os.Environ(), "DISPLAY="+disp)

	titles := []string{
		// Remote Play / streaming
		"Remote Play",
		"PLAY AWAY",
		"Play Away",
		"Play away",
		"Stream games",
		"In-Home Streaming",
		"In Home Streaming",
		"Someone is playing",
		"Удалённая игра",
		// Окна «ОК» / обрыв сессии / Steam
		"Disconnected",
		"Connection failed",
		"Connection Failed",
		"Kicked",
		"You have been kicked",
		"Notice",
		"Information",
		"Confirm",
		"Warning",
		"VAC",
		"You were disconnected",
	}

	now := time.Now()
	for _, t := range titles {
		cmd := exec.Command("xdotool", "search", "--name", t)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) == 0 {
			continue
		}
		wid := fields[len(fields)-1]

		act := exec.Command("xdotool", "windowactivate", "--sync", wid)
		act.Env = env
		if err := act.Run(); err != nil {
			continue
		}
		time.Sleep(90 * time.Millisecond)
		key := exec.Command("xdotool", "key", "--window", wid, "Return")
		key.Env = env
		if err := key.Run(); err != nil {
			continue
		}
		if lastRun != nil {
			*lastRun = now
		}
		log.Printf("[CS2Bot:%s] Auto-dismissed dialog (xdotool title~%q window=%s)", disp, t, wid)
		return
	}
}

func steamDisplayEnv(display int) string {
	if display < 0 {
		return ":0"
	}
	return fmt.Sprintf(":%d", display)
}
