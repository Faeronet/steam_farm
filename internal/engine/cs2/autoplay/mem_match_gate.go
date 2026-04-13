package autoplay

import (
	"os"
	"strings"
	"sync"
)

// SFARM_CS2_MEM_MATCH_GATE: когда не 0/off (по умолчанию включено), SFARM_CS2_MEM_DIAG,
// /snapshot, /stream и /rva-probe собирают тяжёлые данные только в окне «идёт матч после
// первого управляемого спавна» до выхода в меню / gameover / смены без сессии (см. GSI).
// Старт драйвера чтения памяти и sigscan libclient — не от ворот: опрос с первого тика бота.
// Отключить ворота для диагностики вне матча: SFARM_CS2_MEM_MATCH_GATE=0
// Принудительный /rva-probe вне матча: ?force=1
const envCS2MemMatchGate = "SFARM_CS2_MEM_MATCH_GATE"

func memMatchGateEnabled() bool {
	s := strings.TrimSpace(os.Getenv(envCS2MemMatchGate))
	if s == "" {
		return true
	}
	return s != "0" && !strings.EqualFold(s, "off") && !strings.EqualFold(s, "false") && !strings.EqualFold(s, "no")
}

// gsiPawnControllable — персонаж в мире, можно двигаться: playing, hp>0, не freezetime (если раунд в GSI есть).
func gsiPawnControllable(g *GSIState) bool {
	if g == nil || g.Map == nil || g.Map.Name == "" {
		return false
	}
	if g.Player == nil || g.Player.State == nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(g.Player.Activity)) != "playing" {
		return false
	}
	if g.Player.State.Health <= 0 {
		return false
	}
	if g.Round != nil && g.Round.Phase == "freezetime" {
		return false
	}
	return true
}

func gsiMatchSessionAlive(g *GSIState) bool {
	if g == nil || g.Map == nil || strings.TrimSpace(g.Map.Name) == "" {
		return false
	}
	if ph := strings.ToLower(strings.TrimSpace(g.Map.Phase)); ph == "gameover" {
		return false
	}
	if g.Player == nil {
		return false
	}
	act := strings.ToLower(strings.TrimSpace(g.Player.Activity))
	if act == "menu" || act == "textinput" {
		return false
	}
	return act == "playing"
}

type memCollectGateState struct {
	spawnedOnce bool
}

var memCollectGateMu sync.Mutex
var memCollectGateByDisplay = map[int]memCollectGateState{}

func memCollectResetDisplay(display int) {
	memCollectGateMu.Lock()
	delete(memCollectGateByDisplay, display)
	memCollectGateMu.Unlock()
	resetSigScanSkipNotice(display)
}

// Одноразовое сообщение в панель SigScanner: «скан не нужен» — только после ворот матча (см. memDataCollectionActive).
var (
	sigScanSkipNoticeMu   sync.Mutex
	sigScanSkipNoticeSent = map[int]bool{}
)

func resetSigScanSkipNotice(display int) {
	sigScanSkipNoticeMu.Lock()
	delete(sigScanSkipNoticeSent, display)
	sigScanSkipNoticeMu.Unlock()
}

func emitSigScanSkipNoticeIfReady(display int, off cs2MemoryJSON) {
	if offsetsNeedLibclientSigScanFill(off) {
		return
	}
	if !memDataCollectionActive(display) {
		return
	}
	sigScanSkipNoticeMu.Lock()
	if sigScanSkipNoticeSent[display] {
		sigScanSkipNoticeMu.Unlock()
		return
	}
	sigScanSkipNoticeSent[display] = true
	sigScanSkipNoticeMu.Unlock()
	if fn := SigScanLogFunc; fn != nil {
		fn("info", "sigscanner: пропущен — все dw_* RVA уже заданы в конфиге, скан libclient.so не нужен")
	}
}

func memCollectUpdateFromGSI(display int, g *GSIState) {
	if !memMatchGateEnabled() {
		return
	}
	memCollectGateMu.Lock()
	defer memCollectGateMu.Unlock()
	st := memCollectGateByDisplay[display]
	if !gsiMatchSessionAlive(g) {
		st.spawnedOnce = false
		memCollectGateByDisplay[display] = st
		return
	}
	if gsiPawnControllable(g) {
		st.spawnedOnce = true
	}
	memCollectGateByDisplay[display] = st
}

func memDataCollectionActive(display int) bool {
	if !memMatchGateEnabled() {
		return true
	}
	memCollectGateMu.Lock()
	defer memCollectGateMu.Unlock()
	st := memCollectGateByDisplay[display]
	return st.spawnedOnce
}
