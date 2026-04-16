package autoplay

import (
	"math"
	"math/rand"
)

// Виртуальный «коврик» для интеграции WindMouse: крупные координаты, чтобы не упираться в малые числа;
// после достижения цели курсор переносится к центру коврика без движения в игре (как поднять мышь).
const (
	windPadHalf      = 2048.0
	windPadRecJitter = 118.0
	windSettleDist   = 0.22
)

// windMouseState — потоковый WindMouse: гравитация к цели + «ветер», скорость с обрезкой (Ben Land, ben.land/post/2021/04/25/windmouse-human-mouse-movement/).
// Цель (dest) накапливается извне; Step() выдаёт целочисленный относительный шаг в пространстве «виртуального» курсора.
type windMouseState struct {
	curX, curY    float64
	lastQX, lastQY int
	destX, destY  float64
	velX, velY    float64
	windX, windY  float64
	m0            float64
	G0, W0, D0    float64
}

func (w *windMouseState) Reset() {
	cx := windPadHalf
	cy := windPadHalf
	*w = windMouseState{
		// Чуть мягче тяга и шаг — в связке с ease-in цели в smoothMouse.
		G0: 2.45,
		W0: 0.78,
		D0: 8.2,
		m0: 1.92,
		curX:   cx,
		curY:   cy,
		destX:  cx,
		destY:  cy,
		lastQX: int(math.Round(cx)),
		lastQY: int(math.Round(cy)),
	}
}

func (w *windMouseState) ensureInit() {
	if w.G0 <= 0 {
		w.Reset()
	}
}

// AddTarget добавляет желаемое смещение в «градусном» пространстве, уже переведённое в пиксели.
func (w *windMouseState) AddTarget(dx, dy float64) {
	w.ensureInit()
	w.destX += dx
	w.destY += dy
}

func (w *windMouseState) distToDest() float64 {
	return math.Hypot(w.destX-w.curX, w.destY-w.curY)
}

// Step одна интеграция Δt≈1; возвращает относительное смещение в пикселях для MouseMove.
func (w *windMouseState) Step() (moveDx, moveDy int) {
	w.ensureInit()
	dist := w.distToDest()
	if dist < 0.14 {
		return 0, 0
	}

	const sqrt3 = 1.7320508075688772
	const sqrt5 = 2.23606797749979

	wMag := math.Min(w.W0, dist)
	if dist >= w.D0 {
		w.windX = w.windX/sqrt3 + (rand.Float64()*2-1)*wMag/sqrt5
		w.windY = w.windY/sqrt3 + (rand.Float64()*2-1)*wMag/sqrt5
	} else {
		w.windX /= sqrt3
		w.windY /= sqrt3
		if w.m0 < 2.1 {
			w.m0 = 1.85 + rand.Float64()*0.45
		} else {
			w.m0 /= sqrt5
		}
	}

	w.velX += w.windX + w.G0*(w.destX-w.curX)/dist
	w.velY += w.windY + w.G0*(w.destY-w.curY)/dist
	vMag := math.Hypot(w.velX, w.velY)
	if vMag > w.m0 {
		vClip := w.m0/2 + rand.Float64()*(w.m0/2)
		w.velX = (w.velX / vMag) * vClip
		w.velY = (w.velY / vMag) * vClip
	}

	w.curX += w.velX
	w.curY += w.velY

	qx := int(math.Round(w.curX))
	qy := int(math.Round(w.curY))
	moveDx = qx - w.lastQX
	moveDy = qy - w.lastQY
	w.lastQX, w.lastQY = qx, qy
	return
}

// RecenterPadIfSettled переносит внутреннее положение к случайной точке у центра коврика, когда цель уже достигнута.
// HID не двигается (lastQ синхронизирован с cur) — сбрасывается накопленный «ветер» и дрейф, как при подъёме мыши.
func (w *windMouseState) RecenterPadIfSettled() {
	w.ensureInit()
	if w.distToDest() > windSettleDist {
		return
	}
	nx := windPadHalf + (rand.Float64()*2-1)*windPadRecJitter
	ny := windPadHalf + (rand.Float64()*2-1)*windPadRecJitter
	w.curX, w.curY = nx, ny
	w.destX, w.destY = nx, ny
	w.velX, w.velY = 0, 0
	w.windX, w.windY = 0, 0
	w.lastQX = int(math.Round(w.curX))
	w.lastQY = int(math.Round(w.curY))
}
