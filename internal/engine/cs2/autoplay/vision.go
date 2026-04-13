package autoplay

// Lightweight motion-from-centroid assist: frame differencing on a small grayscale ROI.
// No ML — scales to many sandboxes; pairs with combat wave smoothing in bot.go.

const (
	visionROIW         = 160
	visionROIH         = 120
	visionDiffThresh   = 20
	visionMinPixels    = 120
	visionPixelToYaw   = 0.11
	visionPixelToPitch = 0.09
	visionEMA          = 0.38
)

type visionMotion struct {
	prev     []byte
	havePrev bool
	emaCx    float64
	emaCy    float64
}

func (v *visionMotion) Reset() {
	v.havePrev = false
	v.prev = v.prev[:0]
	v.emaCx, v.emaCy = 0, 0
}

// Process returns offset in the same units as smoothMouse “degrees” (before degToPixel).
func (v *visionMotion) Process(gray []byte, w, h int) (yawDeg, pitchDeg float64, ok bool) {
	want := w * h
	if len(gray) != want || w < 16 || h < 16 {
		return 0, 0, false
	}
	if !v.havePrev || len(v.prev) != want {
		v.prev = append(v.prev[:0], gray...)
		v.havePrev = true
		return 0, 0, false
	}

	var sumx, sumy int64
	var count int64
	for i := 0; i < want; i++ {
		d := int(gray[i]) - int(v.prev[i])
		if d < 0 {
			d = -d
		}
		if d > visionDiffThresh {
			y := i / w
			x := i % w
			sumx += int64(x)
			sumy += int64(y)
			count++
		}
	}
	copy(v.prev, gray)

	if count < visionMinPixels {
		return 0, 0, false
	}

	cx := float64(sumx) / float64(count)
	cy := float64(sumy) / float64(count)

	if v.emaCx == 0 && v.emaCy == 0 {
		v.emaCx, v.emaCy = cx, cy
	} else {
		v.emaCx += (cx - v.emaCx) * visionEMA
		v.emaCy += (cy - v.emaCy) * visionEMA
	}

	halfW := float64(w) * 0.5
	halfH := float64(h) * 0.5
	yawDeg = (v.emaCx - halfW) * visionPixelToYaw / halfW * 45
	pitchDeg = (v.emaCy - halfH) * visionPixelToPitch / halfH * 30
	return yawDeg, pitchDeg, true
}
