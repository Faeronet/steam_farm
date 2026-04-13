package autoplay

// EnemyOverlay — подсказки поверх клиента CS2 на том же X11 (x11vnc/VNC показывает слой).
// Реализация: Linux; по умолчанию включён, выкл. переменная SFARM_CS2_OVERLAY — см. overlay_linux.go.
type EnemyOverlay interface {
	Close()
	// PushYolo: xyxy в координатах ROI внутри окна CS2 (YOLO или память: для ESP без YOLO обычно roi=0,0 и rw×rh = gameClient 1280×720).
	PushYolo(roiX, roiY, rw, rh int, viz []YoloDet)
}
