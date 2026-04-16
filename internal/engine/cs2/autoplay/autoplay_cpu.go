package autoplay

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// SFARM_CS2_LOW_CPU: по умолчанию включено — реже тики по умолчанию, реже захват радара/зрения, мягче нагрузка на CPU при нескольких песочницах.
// Выключить для более «отзывчивого» бота: SFARM_CS2_LOW_CPU=0
func cs2AutoplayLowCPU() bool {
	s := strings.TrimSpace(os.Getenv("SFARM_CS2_LOW_CPU"))
	if s == "" {
		return true
	}
	return s != "0" && !strings.EqualFold(s, "off") && !strings.EqualFold(s, "false") && !strings.EqualFold(s, "no")
}

// defaultBotTickHz: при LOW_CPU и без явного SFARM_CS2_BOT_TICK_HZ — реже главный цикл (меньше X11/логики).
func defaultBotTickHz() int {
	if cs2AutoplayLowCPU() {
		return 24
	}
	return 64
}

func intervalRadarPatrol() time.Duration {
	if cs2AutoplayLowCPU() {
		return 360 * time.Millisecond
	}
	return 220 * time.Millisecond
}

func intervalRadarRoam() time.Duration {
	if cs2AutoplayLowCPU() {
		return 320 * time.Millisecond
	}
	return 175 * time.Millisecond
}

func intervalCombatEnemyRGB() time.Duration {
	if cs2AutoplayLowCPU() {
		return 190 * time.Millisecond
	}
	return 95 * time.Millisecond
}

func intervalCombatVision() time.Duration {
	if cs2AutoplayLowCPU() {
		return 220 * time.Millisecond
	}
	return 115 * time.Millisecond
}

func focusRefreshInterval() time.Duration {
	if cs2AutoplayLowCPU() {
		return 10 * time.Second
	}
	return 5 * time.Second
}

// SFARM_CS2_SPAWN_ENTER_INTERVAL_MS: редкий Enter при ожидании спавна после смены карты / 5b (0 = не жать).
// Частые Enter ломают выбор команды / ведут в наблюдатели.
func spawnEnterInterval() time.Duration {
	s := strings.TrimSpace(os.Getenv("SFARM_CS2_SPAWN_ENTER_INTERVAL_MS"))
	if s == "" || s == "0" {
		return 0
	}
	ms, err := strconv.Atoi(s)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// SFARM_CS2_PHASE1_WINDOW_GRACE_SEC: если sandbox уже сообщил PID cs2, но X11 не видит окно с «Counter-Strike» в заголовке,
// через N секунд всё равно перейти к фазе 2 (иначе вечный wait-process при hidepid/другом WM_NAME). 0 = строго ждать окно+процесс.
func phase1WindowGraceDuration() time.Duration {
	s := strings.TrimSpace(os.Getenv("SFARM_CS2_PHASE1_WINDOW_GRACE_SEC"))
	if s == "0" {
		return 0
	}
	if s == "" {
		return 55 * time.Second
	}
	sec, err := strconv.Atoi(s)
	if err != nil || sec < 0 {
		return 55 * time.Second
	}
	return time.Duration(sec) * time.Second
}

// SFARM_CS2_SKIP_WINDOW_CHECK=1 — фаза 1 достаточно isCS2RunningOnDisplay (окно не требуется). Только для отладки.
func cs2SkipWindowCheck() bool {
	v := strings.TrimSpace(os.Getenv("SFARM_CS2_SKIP_WINDOW_CHECK"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
