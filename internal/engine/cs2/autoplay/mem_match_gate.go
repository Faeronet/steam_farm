package autoplay

import (
	"os"
	"strings"
	"sync"
	"time"
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

// gsiActivityInWorld — не главное меню / ввод текста; «в мире» по activity=playing или по пустому
// activity при загруженной карте (CS2 часто не шлёт строку "playing" на сервере).
func gsiActivityInWorld(g *GSIState) bool {
	if g == nil || g.Player == nil {
		return false
	}
	act := strings.ToLower(strings.TrimSpace(g.Player.Activity))
	if act == "menu" || act == "textinput" {
		return false
	}
	if act == "playing" {
		return true
	}
	if g.Map == nil || strings.TrimSpace(g.Map.Name) == "" {
		return false
	}
	return act == ""
}

// gsiPlayableSide — боевая сторона T/CT; Spectator / прочее — не считаем «готов к автоплею».
// Пустая Team: старый GSI без player_team — не отсекаем (иначе всегда false).
func gsiPlayableSide(g *GSIState) bool {
	if g == nil || g.Player == nil {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(g.normalizedTeam()))
	if t == "" {
		return true
	}
	switch t {
	case "t", "ct", "terrorist", "counter-terrorist", "counterterrorist":
		return true
	default:
		return false
	}
}

// gsiHasExplicitPlayableTeam — в GSI явно пришла T/CT (удобно для таймингов без «угадывания»).
func gsiHasExplicitPlayableTeam(g *GSIState) bool {
	if g == nil || g.Player == nil {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(g.normalizedTeam()))
	switch t {
	case "t", "ct", "terrorist", "counter-terrorist", "counterterrorist":
		return true
	default:
		return false
	}
}

// spawnTeamSelectMinWait — после смены карты / входа в 5b console «round_start» часто раньше, чем UI выбора команды.
// Пока в GSI нет явной T|CT, не завершаем ожидание раньше этого окна (Enter тогда уводит в зрители).
const spawnTeamSelectMinWait = 2800 * time.Millisecond

// spawnLoadoutTimingOK — можно считать спавн «подтверждённым» для автоплея (есть команда в GSI или прошла пауза).
func spawnLoadoutTimingOK(phaseStart time.Time, g *GSIState) bool {
	if gsiHasExplicitPlayableTeam(g) {
		return true
	}
	if phaseStart.IsZero() {
		return false
	}
	return time.Since(phaseStart) >= spawnTeamSelectMinWait
}

// gsiPawnControllable — персонаж в мире, можно двигаться: in-world activity, hp>0, не freezetime (если раунд в GSI есть), не зритель.
func gsiPawnControllable(g *GSIState) bool {
	if g == nil || g.Map == nil || g.Map.Name == "" {
		return false
	}
	if g.Player == nil || g.Player.State == nil {
		return false
	}
	if !gsiActivityInWorld(g) {
		return false
	}
	if !gsiPlayableSide(g) {
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
	if act == "playing" {
		return true
	}
	// Сессия матча на карте без явного "playing" (типично для CS2 онлайн).
	return act == ""
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
