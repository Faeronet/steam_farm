//go:build !linux

package autoplay

func NewEnemyOverlay(display int) EnemyOverlay {
	return nil
}
