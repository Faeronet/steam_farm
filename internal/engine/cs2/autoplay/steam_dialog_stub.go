//go:build !linux

package autoplay

import "time"

func maybeDismissSteamDialogs(_ int, _ *time.Time) {}

func dismissSteamLikeDialogs(_ int, _ *time.Time, _ bool) {}
