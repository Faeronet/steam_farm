package autoplay

import (
	"sync"
)

// sfarm-sandbox (root) эмитит cs2_pid в IPC; desktop (не root) не видит cs2 через pgrep при hidepid.
var sandboxReportedCS2PID sync.Map // int64 accountID -> int pid

func SetSandboxReportedCS2PID(accountID int64, pid int) {
	if accountID <= 0 || pid <= 0 {
		return
	}
	sandboxReportedCS2PID.Store(accountID, pid)
}

func ClearSandboxReportedCS2PID(accountID int64) {
	sandboxReportedCS2PID.Delete(accountID)
}

func sandboxReportedCS2PIDAlive(accountID int64) (int, bool) {
	if accountID <= 0 {
		return 0, false
	}
	v, ok := sandboxReportedCS2PID.Load(accountID)
	if !ok {
		return 0, false
	}
	pid, ok := v.(int)
	if !ok || pid <= 0 {
		return 0, false
	}
	// Не проверяем /proc: при hidepid десктоп не видит чужие PID, Stat даёт ложный «мертв».
	// Живость снимается через cs2_pid:0 от sandbox или exited.
	return pid, true
}
