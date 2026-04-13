//go:build !linux

package autoplay

var SigScanLogFunc func(level, msg string)

func sigScanLibclient(pid int, imageBase uint64, modulePath string) (map[string]uint64, error) {
	return nil, nil
}

func applySigScanToOffsets(off *cs2MemoryJSON, found map[string]uint64) int {
	return 0
}

func SigScanInvalidateCache() {}
