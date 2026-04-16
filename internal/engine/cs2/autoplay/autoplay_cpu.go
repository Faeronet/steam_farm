package autoplay

import (
	"os"
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
