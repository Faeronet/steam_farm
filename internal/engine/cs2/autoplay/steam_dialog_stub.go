//go:build !linux

package autoplay

import "time"

func maybeDismissSteamDialogs(_ *InputSender, _ int, _ *time.Time) bool { return false }

func dismissSteamLikeDialogs(_ int, _ *time.Time, _ bool) bool { return false }
