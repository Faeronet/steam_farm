package autoplay

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

var yoloPreviewMagic = []byte{'Y', 'L', 'O', 'P'}

// YoloPreviewTCPPort локальный порт превью для дисплея :N (один бот — одно окно).
func YoloPreviewTCPPort(display int) int {
	return 37800 + (display % 700)
}

// YoloPreviewSink шлёт на yolo_preview.py живой RGB-ROI и боксы (то же, что ушло в YOLO worker).
type YoloPreviewSink struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

func NewYoloPreviewSink(display int) *YoloPreviewSink {
	return &YoloPreviewSink{
		addr: fmt.Sprintf("127.0.0.1:%d", YoloPreviewTCPPort(display)),
	}
}

func yoloClsByte(cls string) byte {
	switch cls {
	case "c":
		return 0
	case "ch":
		return 1
	case "t":
		return 2
	case "th":
		return 3
	default:
		return 255
	}
}

// Push: по детекции 32 байта — 4×float32 xyxy, float32 conf, uint8 cls (0–3 CS2 / 255), 11 байт ASCII имени (COCO).
// Push отправляет один кадр (после инференса; для превью обычно viz — все классы).
func (s *YoloPreviewSink) Push(rgb []byte, w, h int, dets []YoloDet) {
	if s == nil || w <= 0 || h <= 0 || len(rgb) != w*h*3 {
		return
	}
	var valid []YoloDet
	for i := range dets {
		if len(dets[i].Xyxy) >= 4 {
			valid = append(valid, dets[i])
		}
	}
	n := len(valid)

	var pkt bytes.Buffer
	pkt.Write(yoloPreviewMagic)
	_ = binary.Write(&pkt, binary.LittleEndian, uint32(w))
	_ = binary.Write(&pkt, binary.LittleEndian, uint32(h))
	_ = binary.Write(&pkt, binary.LittleEndian, uint32(n))
	for i := range valid {
		d := &valid[i]
		for j := 0; j < 4; j++ {
			_ = binary.Write(&pkt, binary.LittleEndian, float32(d.Xyxy[j]))
		}
		_ = binary.Write(&pkt, binary.LittleEndian, float32(d.Conf))
		cb := yoloClsByte(d.Cls)
		_ = pkt.WriteByte(cb)
		var name [11]byte
		if cb == 255 && d.Cls != "" {
			copy(name[:], []byte(d.Cls))
		}
		_, _ = pkt.Write(name[:])
	}
	pkt.Write(rgb)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		c, err := net.DialTimeout("tcp", s.addr, 2*time.Second)
		if err != nil {
			return
		}
		s.conn = c
	}
	if _, err := s.conn.Write(pkt.Bytes()); err != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *YoloPreviewSink) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}
