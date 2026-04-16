//go:build linux

package autoplay

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const steamDialogDismissCooldown = 3 * time.Second

// maybeDismissSteamDialogs закрывает Steam Cloud, CS2 «Disconnected», Steam Remote Play и т.д.
// Возвращает true, если закрыто окно, после которого нужен повторный поиск матча (Disconnected / kick / connection lost).
func maybeDismissSteamDialogs(input *InputSender, display int, lastRun *time.Time) (needsRematch bool) {
	// Облако: НЕ за общим cooldown с disconnect-диалогами — иначе после любого lastRun 3 с не пробуем Cloud,
	// а ложный «успех» (fallback-клик мимо) гасит все попытки.
	if input != nil && input.TryDismissSteamCloudDialog() {
		if lastRun != nil {
			*lastRun = time.Now()
		}
		log.Printf("[CS2Bot:%s] Auto-dismissed Steam Cloud dialog (X11/XTest)", steamDisplayEnv(display))
		return false
	}
	if strings.TrimSpace(os.Getenv("SFARM_STEAM_XDOTOOL")) != "0" {
		if _, err := exec.LookPath("xdotool"); err == nil {
			if dismissSteamCloudDialogs(display) {
				if lastRun != nil {
					*lastRun = time.Now()
				}
				return false
			}
		}
	}
	// Прочие диалоги (xdotool) — с cooldown.
	if lastRun != nil && time.Since(*lastRun) < steamDialogDismissCooldown {
		return false
	}
	if strings.TrimSpace(os.Getenv("SFARM_STEAM_XDOTOOL")) == "0" {
		return false
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return false
	}
	return dismissSteamLikeDialogs(display, lastRun, false)
}

func steamDisplayEnv(display int) string {
	if display < 0 {
		return ":0"
	}
	return fmt.Sprintf(":%d", display)
}

// x11DisplayCandidatesForXdotool совпадает с порядком в input.go (worker): unix :N, затем TCP 127.0.0.1:N.0.
// Xvfb в sandbox слушает TCP (process.rs -listen tcp); если у sfarm-desktop другой /tmp, сокета нет — ввод уже идёт по TCP, а xdotool с одним «:N» смотрел не на тот сервер.
func x11DisplayCandidatesForXdotool(display int) []string {
	if v := strings.TrimSpace(os.Getenv("SFARM_X11_DISPLAY")); v != "" {
		return []string{v}
	}
	if display < 0 {
		return []string{":0"}
	}
	return []string{
		fmt.Sprintf(":%d", display),
		fmt.Sprintf("127.0.0.1:%d.0", display),
	}
}

// xdotoolSearchWIDs ищет окна по подстроке WM_NAME; сначала --all (дочерние/модальные окна Steam часто не «onlyvisible»).
func xdotoolSearchWIDs(env []string, nameSubstr string) []string {
	var tries [][]string
	for _, limit := range []string{"64", "32", "12"} {
		tries = append(tries, []string{"search", "--all", "--name", "--limit", limit, nameSubstr})
		tries = append(tries, []string{"search", "--onlyvisible", "--name", "--limit", limit, nameSubstr})
		tries = append(tries, []string{"search", "--name", "--limit", limit, nameSubstr})
	}
	for _, args := range tries {
		cmd := exec.Command("xdotool", args...)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) > 0 {
			return fields
		}
	}
	return nil
}

func xdotoolWindowName(env []string, wid string) string {
	cmd := exec.Command("xdotool", "getwindowname", wid)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// xdotoolAllWindowIDs — полный перебор WM_NAME (regex), чтобы поймать заголовки вне search --limit.
func xdotoolAllWindowIDs(env []string) []string {
	for _, pat := range []string{".*", "."} {
		cmd := exec.Command("xdotool", "search", "--all", "--name", pat, "--limit", "512")
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) > 0 {
			return fields
		}
	}
	return nil
}

func looksLikeSteamCloudSaveDialogTitle(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	hasCloud := strings.Contains(n, "cloud") || strings.Contains(n, "облако")
	if !hasCloud {
		return false
	}
	return strings.Contains(n, "out of date") ||
		strings.Contains(n, "outdated") ||
		strings.Contains(n, "not yet") ||
		strings.Contains(n, "upload") ||
		strings.Contains(n, "conflict") ||
		strings.Contains(n, "sync") ||
		strings.Contains(n, "save") ||
		strings.Contains(n, "устар") ||
		strings.Contains(n, "синхрон") ||
		strings.Contains(n, "конфликт")
}

func steamCloudDialogWIDsByNameScan(env []string) []string {
	wids := xdotoolAllWindowIDs(env)
	if len(wids) == 0 {
		return nil
	}
	debug := strings.TrimSpace(os.Getenv("SFARM_STEAM_XDOTOOL_DEBUG")) == "1"
	var out []string
	seen := make(map[string]struct{})
	for _, wid := range wids {
		name := xdotoolWindowName(env, wid)
		if debug && strings.Contains(strings.ToLower(name), "cloud") {
			log.Printf("[CS2Bot][xdotool-debug] wid=%s name=%q", wid, name)
		}
		if !looksLikeSteamCloudSaveDialogTitle(name) {
			continue
		}
		if _, ok := seen[wid]; ok {
			continue
		}
		seen[wid] = struct{}{}
		out = append(out, wid)
	}
	return out
}

func xdotoolSearchWIDsByClass(env []string, classSubstr string) []string {
	var tries [][]string
	for _, limit := range []string{"64", "32"} {
		tries = append(tries, []string{"search", "--all", "--class", classSubstr, "--limit", limit})
		tries = append(tries, []string{"search", "--onlyvisible", "--class", classSubstr, "--limit", limit})
	}
	for _, args := range tries {
		cmd := exec.Command("xdotool", args...)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) > 0 {
			return fields
		}
	}
	return nil
}

func raiseAndActivateWindow(env []string, wid string) bool {
	for _, args := range [][]string{
		{"windowmap", wid},
		{"windowraise", wid},
	} {
		cmd := exec.Command("xdotool", args...)
		cmd.Env = env
		_ = cmd.Run()
	}
	act := exec.Command("xdotool", "windowactivate", "--sync", wid)
	act.Env = env
	return act.Run() == nil
}

func tryDismissWindowKeys(env []string, wid string, keys ...string) bool {
	if !raiseAndActivateWindow(env, wid) {
		return false
	}
	time.Sleep(110 * time.Millisecond)
	args := append([]string{"key", "--window", wid}, keys...)
	cmd := exec.Command("xdotool", args...)
	cmd.Env = env
	return cmd.Run() == nil
}

func tryDismissWindow(env []string, wid string) bool {
	return tryDismissWindowKeys(env, wid, "Return")
}

// Согласовано с input.go (C): по умолчанию 2×Tab+Return; SFARM_STEAM_CLOUD_TAB_COUNT; только Enter — SFARM_STEAM_CLOUD_KEYS=return.
func steamCloudDismissKeyArgs() []string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SFARM_STEAM_CLOUD_KEYS")), "return") {
		return []string{"Return"}
	}
	n := 3
	if s := strings.TrimSpace(os.Getenv("SFARM_STEAM_CLOUD_TAB_COUNT")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 && v <= 8 {
			n = v
		}
	}
	out := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		out = append(out, "Tab")
	}
	out = append(out, "Return")
	return out
}

// dismissSteamCloudDialogs — «Cloud Out of Date» / конфликт облака — «Play anyway».
func dismissSteamCloudDialogs(display int) bool {
	for _, disp := range x11DisplayCandidatesForXdotool(display) {
		env := append(os.Environ(), "DISPLAY="+disp)
		if dismissSteamCloudDialogsOnEnv(disp, env) {
			return true
		}
	}
	return false
}

func dismissSteamCloudDialogsOnEnv(disp string, env []string) bool {
	titles := []string{
		"Cloud Out of Date",
		"Cloud out of date",
		"cloud out of date",
		"Облако",
		"облако",
	}

	tryCloud := func(wid string, via string) bool {
		ok := tryDismissWindowKeys(env, wid, steamCloudDismissKeyArgs()...)
		if !ok {
			return false
		}
		log.Printf("[CS2Bot:%s] Auto-dismissed Steam Cloud dialog (via %s wid=%s name=%q)", disp, via, wid, xdotoolWindowName(env, wid))
		return true
	}

	for _, t := range titles {
		for _, wid := range xdotoolSearchWIDs(env, t) {
			if tryCloud(wid, "search:"+t) {
				return true
			}
		}
	}
	for _, wid := range steamCloudDialogWIDsByNameScan(env) {
		if tryCloud(wid, "scan") {
			return true
		}
	}
	for _, cls := range []string{"Steam", "steam"} {
		for _, wid := range xdotoolSearchWIDsByClass(env, cls) {
			name := xdotoolWindowName(env, wid)
			if !looksLikeSteamCloudSaveDialogTitle(name) {
				continue
			}
			if tryCloud(wid, "class:"+cls) {
				return true
			}
		}
	}
	return false
}

func patternNeedsRematch(pattern string) bool {
	p := strings.ToLower(pattern)
	return strings.Contains(p, "disconnect") ||
		strings.Contains(p, "connection failed") ||
		strings.Contains(p, "kicked") ||
		strings.Contains(p, "you were disconnected") ||
		strings.Contains(p, "remote host") ||
		strings.Contains(p, "отключ")
}

// dismissSteamLikeDialogs — CS2 «Disconnected», Steam Remote Play, OK-диалоги. Возвращает true = нужен re-queue матча.
func dismissSteamLikeDialogs(display int, lastRun *time.Time, force bool) bool {
	if !force && lastRun != nil && time.Since(*lastRun) < steamDialogDismissCooldown {
		return false
	}
	if strings.TrimSpace(os.Getenv("SFARM_STEAM_XDOTOOL")) == "0" {
		return false
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return false
	}

	titles := []string{
		"Remote Play",
		"PLAY AWAY",
		"Play Away",
		"Play away",
		"Stream games",
		"In-Home Streaming",
		"In Home Streaming",
		"Someone is playing",
		"Удалённая игра",
		"another device",
		"Another device",
		"currently playing",
		"Launching Counter-Strike",
		"play here instead",
		"The remote host",
		"remote host closed",
		"Disconnected",
		"Отключено",
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
	for _, disp := range x11DisplayCandidatesForXdotool(display) {
		env := append(os.Environ(), "DISPLAY="+disp)
		for _, t := range titles {
			wids := xdotoolSearchWIDs(env, t)
			for _, wid := range wids {
				if !tryDismissWindow(env, wid) {
					continue
				}
				if lastRun != nil {
					*lastRun = now
				}
				rem := patternNeedsRematch(t)
				log.Printf("[CS2Bot:%s] Auto-dismissed dialog (xdotool name~%q wid=%s rematch=%v)", disp, t, wid, rem)
				return rem
			}
		}
	}
	return false
}
