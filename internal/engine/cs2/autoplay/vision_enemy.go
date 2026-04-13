package autoplay

import "math"

// enemyROIGame — широкий центральный кадр (не только прицел): террористы могут быть в полный рост по бокам.
func enemyROIGame() (x0, y0, rw, rh int) {
	const refCX, refCY = 512, 336
	const refHalfW, refHalfH = 480, 340
	cx := refCX * gameClientW / menuRefW
	cy := refCY * gameClientH / menuRefH
	rw = 2 * refHalfW * gameClientW / menuRefW
	rh = 2 * refHalfH * gameClientH / menuRefH
	x0 = cx - rw/2
	y0 = cy - rh/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	return
}

// DetectTerroristWarmAim ищет тёплые кожаные/оливковые тона (T-модели), без привязки к одному цвету — фильтр неба и белых стен.
// Возвращает смещение прицела в тех же «градусных» единицах, что smoothMouse (масштаб умеренный).
func DetectTerroristWarmAim(rgb []byte, w, h int) (yawDeg, pitchDeg, confidence float64, ok bool) {
	if len(rgb) != w*h*3 || w < 16 || h < 16 {
		return 0, 0, 0, false
	}

	x0 := w / 10
	x1 := w - w/10
	y0 := h / 12
	y1 := h - h/10

	var sx, sy int64
	var n int64
	for y := y0; y < y1; y++ {
		row := y * w * 3
		for x := x0; x < x1; x++ {
			i := row + x*3
			if isTerroristToneRGB(rgb[i], rgb[i+1], rgb[i+2]) {
				sx += int64(x)
				sy += int64(y)
				n++
			}
		}
	}

	area := float64((x1 - x0) * (y1 - y0))
	minPix := int64(math.Max(120, area*0.0012))
	if n < minPix {
		return 0, 0, 0, false
	}

	cx := float64(sx) / float64(n)
	cy := float64(sy) / float64(n)
	halfW := float64(w) * 0.5
	halfH := float64(h) * 0.5
	yawDeg = (cx - halfW) / halfW * 42
	pitchDeg = (cy - halfH) / halfH * 28
	confidence = math.Min(1, float64(n)/area*25)
	return yawDeg, pitchDeg, confidence, true
}

func isTerroristToneRGB(b, g, r byte) bool {
	ri, gi, bi := int(r), int(g), int(b)
	if bi > ri+45 && bi > gi+28 {
		return false
	}
	if ri < 48 && gi < 48 {
		return false
	}
	maxv := ri
	if gi > maxv {
		maxv = gi
	}
	if bi > maxv {
		maxv = bi
	}
	minv := ri
	if gi < minv {
		minv = gi
	}
	if bi < minv {
		minv = bi
	}
	sat := maxv - minv
	if sat < 18 {
		return false
	}
	if ri < gi-8 {
		return false
	}
	if ri < bi-5 && gi < bi+15 {
		return false
	}
	if ri > 238 && gi > 232 && bi > 228 {
		return false
	}
	return true
}
