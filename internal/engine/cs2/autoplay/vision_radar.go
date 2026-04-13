package autoplay

import "math"

// AnalyzeRadarMinimapRGB — CS2 HUD: непрозрачный чёрный void, цветной пол, иконка игрока (cyan/yellow) + короткая белая стрелка.
// Широкий белый конус FOV отрезается кольцом вокруг центра иконки — иначе «вперёд» ошибочно тянет в конус.
func AnalyzeRadarMinimapRGB(rgb []byte, w, h int, emaWall *float64) (wallAhead, openLeft, openRight, yawSuggest float64, ok bool) {
	want := 3 * w * h
	if len(rgb) != want || w < 32 || h < 32 {
		return 0, 0, 0, 0, false
	}
	cx := float64(w-1) * 0.5
	cy := float64(h-1) * 0.5
	rad := float64(minInt(w, h)) * 0.475
	rad2 := rad * rad
	inCircle := func(x, y int) bool {
		dx := float64(x) - cx
		dy := float64(y) - cy
		return dx*dx+dy*dy <= rad2
	}
	isPlayer := func(b, g, r int) bool {
		// CT / ally teal-cyan: высокий B и G.
		if b >= 68 && g >= 54 && r <= 210 && b > r+12 && g > r+2 && (b+g > r+r+48) {
			return true
		}
		// T / жёлто-оранжевая иконка (Mirage и др.).
		if r >= 92 && g >= 78 && b <= minInt(r, g)-12 && r+g > 200 {
			return true
		}
		return false
	}
	var psx, psy int64
	var nPl int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !inCircle(x, y) {
				continue
			}
			i := (y*w + x) * 3
			b0, g0, r0 := int(rgb[i]), int(rgb[i+1]), int(rgb[i+2])
			if isPlayer(b0, g0, r0) {
				psx += int64(x)
				psy += int64(y)
				nPl++
			}
		}
	}
	if nPl < 5 {
		return 0, 0, 0, 0, false
	}
	pcx := float64(psx) / float64(nPl)
	pcy := float64(psy) / float64(nPl)

	const whiteThr = 186
	minD2 := 5.0 * 5.0
	maxD2 := 42.0 * 42.0
	var tipX, tipY float64
	maxDD := 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !inCircle(x, y) {
				continue
			}
			i := (y*w + x) * 3
			b0, g0, r0 := int(rgb[i]), int(rgb[i+1]), int(rgb[i+2])
			if r0 < whiteThr || g0 < whiteThr || b0 < whiteThr {
				continue
			}
			dx := float64(x) - pcx
			dy := float64(y) - pcy
			d2 := dx*dx + dy*dy
			if d2 < minD2 || d2 > maxD2 {
				continue
			}
			if d2 > maxDD {
				maxDD = d2
				tipX, tipY = float64(x), float64(y)
			}
		}
	}
	if maxDD < 10*10 {
		return 0, 0, 0, 0, false
	}
	ux := tipX - pcx
	uy := tipY - pcy
	norm := math.Hypot(ux, uy)
	if norm < 2.5 {
		return 0, 0, 0, 0, false
	}
	ux /= norm
	uy /= norm
	// Точка сэмпла стен чуть «зад» стрелки, к центру иконки.
	px := pcx - ux*10.5
	py := pcy - uy*10.5

	isVoid := func(ix, iy int) bool {
		if ix < 0 || iy < 0 || ix >= w || iy >= h {
			return true
		}
		if !inCircle(ix, iy) {
			return true
		}
		j := (iy*w + ix) * 3
		b0, g0, r0 := int(rgb[j]), int(rgb[j+1]), int(rgb[j+2])
		return r0+g0+b0 < 52
	}

	sampleWall := func(drx, dry float64) float64 {
		var hits, samples int
		for step := 2; step <= 28; step += 2 {
			fx := px + drx*float64(step)*0.5
			fy := py + dry*float64(step)*0.5
			ix := int(fx + 0.5)
			iy := int(fy + 0.5)
			if ix < 1 || iy < 1 || ix >= w-1 || iy >= h-1 {
				hits++
				samples++
				continue
			}
			samples++
			if isVoid(ix, iy) {
				hits++
			}
		}
		if samples == 0 {
			return 0
		}
		return float64(hits) / float64(samples)
	}

	wallAhead = sampleWall(ux, uy)
	lx, ly := -uy, ux
	rx_, ry_ := uy, -ux
	flx := ux + lx
	fly := uy + ly
	frx := ux + rx_
	fry := uy + ry_
	if nl := math.Hypot(flx, fly); nl > 1e-6 {
		flx /= nl
		fly /= nl
	}
	if nr := math.Hypot(frx, fry); nr > 1e-6 {
		frx /= nr
		fry /= nr
	}
	openLeft = 1 - sampleWall(flx, fly)
	openRight = 1 - sampleWall(frx, fry)

	if emaWall != nil {
		*emaWall = 0.5**emaWall + 0.5*wallAhead
		wallAhead = *emaWall
	}

	if wallAhead > 0.24 {
		if openLeft > openRight+0.10 {
			yawSuggest = 58
		} else if openRight > openLeft+0.10 {
			yawSuggest = -58
		} else if openLeft >= openRight {
			yawSuggest = 48
		} else {
			yawSuggest = -48
		}
	}

	return wallAhead, openLeft, openRight, yawSuggest, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stretchRadarGray растягивает гистограмму яркости по ROI миникарты — проще отделить стрелку/иконки от фона после «призрачного» HUD.
func stretchRadarGray(gray []byte) {
	if len(gray) < 256 {
		return
	}
	minG, maxG := gray[0], gray[0]
	for _, g := range gray {
		if g < minG {
			minG = g
		}
		if g > maxG {
			maxG = g
		}
	}
	span := int(maxG) - int(minG)
	if span < 12 {
		return
	}
	scale := 255.0 / float64(span)
	bias := float64(minG)
	for i := range gray {
		v := (float64(gray[i]) - bias) * scale
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		gray[i] = byte(v + 0.5)
	}
}

// Radar HUD layout: baseline Panorama ref 1024×598 scaled to game client 1280×720 (matches menuCoord / sandbox Xvfb).
const (
	radarMenuRefLeft = 18
	radarMenuRefTop  = 44
	radarMenuRefSize = 228
	menuRefW         = 1024
	menuRefH         = 598
	gameClientW      = 1280
	gameClientH      = 720
)

// radarROIGame returns top-left and size of the square HUD region that contains the circular minimap + player icon.
func radarROIGame() (x0, y0, rw, rh int) {
	x0 = radarMenuRefLeft * gameClientW / menuRefW
	y0 = radarMenuRefTop * gameClientH / menuRefH
	rw = radarMenuRefSize * gameClientW / menuRefW
	rh = radarMenuRefSize * gameClientH / menuRefH
	return
}

type radarMinimapNav struct {
	emaWall float64
}

func (r *radarMinimapNav) Reset() { r.emaWall = 0 }

// AnalyzeRadarMinimap uses grayscale only (fallback): без растяжения — иначе белый конус FOV сливается со «стрелкой».
// Предпочтительно AnalyzeRadarMinimapRGB.
func AnalyzeRadarMinimap(gray []byte, w, h int, emaWall *float64) (wallAhead, openLeft, openRight, yawSuggest float64, ok bool) {
	want := w * h
	if len(gray) != want || w < 32 || h < 32 {
		return 0, 0, 0, 0, false
	}

	maxG := byte(0)
	for _, g := range gray {
		if g > maxG {
			maxG = g
		}
	}
	thr := int(maxG) - 28
	if thr < 168 {
		thr = 168
	}

	var sumX, sumY int64
	var nArrow int
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			if int(gray[row+x]) >= thr {
				sumX += int64(x)
				sumY += int64(y)
				nArrow++
			}
		}
	}
	if nArrow < 10 || nArrow > (w*h)/3 {
		return 0, 0, 0, 0, false
	}

	mx := float64(sumX) / float64(nArrow)
	my := float64(sumY) / float64(nArrow)

	var tipX, tipY float64
	maxD := 0.0
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			if int(gray[row+x]) >= thr {
				dx := float64(x) - mx
				dy := float64(y) - my
				d := dx*dx + dy*dy
				if d > maxD {
					maxD = d
					tipX, tipY = float64(x), float64(y)
				}
			}
		}
	}
	if maxD < 16 {
		return 0, 0, 0, 0, false
	}

	ux := tipX - mx
	uy := tipY - my
	norm := math.Hypot(ux, uy)
	if norm < 1.5 {
		return 0, 0, 0, 0, false
	}
	ux /= norm
	uy /= norm

	// Icon centroid sits slightly behind the arrow along −forward.
	px := mx - ux*11
	py := my - uy*11

	const wallDark = 58
	sampleWall := func(dx, dy float64) float64 {
		var hits, samples int
		for step := 3; step <= 27; step += 3 {
			fx := px + dx*float64(step)*0.48
			fy := py + dy*float64(step)*0.48
			ix := int(fx + 0.5)
			iy := int(fy + 0.5)
			if ix < 1 || iy < 1 || ix >= w-1 || iy >= h-1 {
				break
			}
			samples++
			g := int(gray[iy*w+ix])
			if g < wallDark {
				hits++
			}
			gl := int(gray[iy*w+ix-1])
			gr := int(gray[iy*w+ix+1])
			gu := int(gray[(iy-1)*w+ix])
			gd := int(gray[(iy+1)*w+ix])
			if absInt(gl-gr)+absInt(gu-gd) > 88 && g < 125 {
				hits++
			}
		}
		if samples == 0 {
			return 0
		}
		return float64(hits) / float64(samples)
	}

	wallAhead = sampleWall(ux, uy)

	lx, ly := -uy, ux
	rx, ry := uy, -ux
	flx := ux + lx
	fly := uy + ly
	frx := ux + rx
	fry := uy + ry
	if nl := math.Hypot(flx, fly); nl > 1e-6 {
		flx /= nl
		fly /= nl
	}
	if nr := math.Hypot(frx, fry); nr > 1e-6 {
		frx /= nr
		fry /= nr
	}
	openLeft = 1 - sampleWall(flx, fly)
	openRight = 1 - sampleWall(frx, fry)

	if emaWall != nil {
		*emaWall = 0.52**emaWall + 0.48*wallAhead
		wallAhead = *emaWall
	}

	if wallAhead > 0.28 {
		if openLeft > openRight+0.11 {
			yawSuggest = 52
		} else if openRight > openLeft+0.11 {
			yawSuggest = -52
		} else if openLeft >= openRight {
			yawSuggest = 44
		} else {
			yawSuggest = -44
		}
	}

	return wallAhead, openLeft, openRight, yawSuggest, true
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
