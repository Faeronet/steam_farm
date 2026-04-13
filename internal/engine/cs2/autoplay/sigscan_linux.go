//go:build linux

package autoplay

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sigOp describes a post-match operation (applied sequentially).
type sigOp struct {
	kind     string // "rip", "add", "slice"
	offset   int    // rip: byte offset of disp32 inside match (default 3)
	instrLen int    // rip: total instruction length (default 7)
	addVal   int64  // add: signed constant
	slStart  int    // slice: start byte (inclusive)
	slEnd    int    // slice: end byte (exclusive), max 4 for uint32
}

type sigPattern struct {
	name string
	pat  []byte // concrete bytes (wildcard positions ignored during match)
	mask []byte // 0xFF = exact, 0x00 = wildcard
	ops  []sigOp
}

// Linux ELF patterns from cs2-dumper linux branch config.json.
// ? bytes in the original notation become 0x00 in pat with mask 0x00.
var libclientSigPatterns = []sigPattern{
	makeSig("dwEntityList",
		"4C 89 25 ?? ?? ?? ?? 48 89 05 ?? ?? ?? ?? 49 8B 5D",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwLocalPlayerController",
		"4C 89 2D ?? ?? ?? ?? E8 ?? ?? ?? ?? 48 8B 45",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	// dwPrediction; dwLocalPlayerPawn = dwPrediction + 328
	makeSig("dwLocalPlayerPawn",
		"48 8D 05 ?? ?? ?? ?? C3 0F 1F 84 00 ?? ?? ?? ?? C7 47 ?? ?? ?? ?? ?? C7 47 ?? ?? ?? ?? ?? C3",
		sigOp{kind: "rip", offset: 3, instrLen: 7},
		sigOp{kind: "add", addVal: 328}),

	makeSig("dwViewMatrix",
		"48 8D 05 ?? ?? ?? ?? C3 0F 1F 84 00 ?? ?? ?? ?? 83 FF",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwGameEntitySystem",
		"48 89 3D ?? ?? ?? ?? E9 ?? ?? ?? ?? 55",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwGameEntitySystem_highestEntityIndex",
		"8B 87 ?? ?? ?? ?? C3 66 0F 1F 84 00 ?? ?? ?? ?? 8B 97",
		sigOp{kind: "slice", slStart: 2, slEnd: 4}),

	makeSig("dwGlobalVars",
		"48 89 35 ?? ?? ?? ?? 48 89 46",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwGameRules",
		"48 89 1D ?? ?? ?? ?? 48 8B 00",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwCSGOInput",
		"48 8D 05 ?? ?? ?? ?? C3 0F 1F 84 00 ?? ?? ?? ?? 55 48 89 E5 41 56 41 55 49 89 FD 41 54 49 89 F4",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwViewRender",
		"48 8D 05 ?? ?? ?? ?? 48 89 38 48 85 FF",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwGlowManager",
		"48 8B 05 ?? ?? ?? ?? C3 0F 1F 84 00 ?? ?? ?? ?? 48 8D 05 ?? ?? ?? ?? 48 C7 47",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),

	makeSig("dwSensitivity",
		"48 8B 05 ?? ?? ?? ?? 48 8B 40 ?? E9 ?? ?? ?? ?? 48 8B 05 ?? ?? ?? ?? 48 8B 40 ?? EB ?? 0F 1F 00 55 48 89 E5 41 57 66 41 0F 7E DF",
		sigOp{kind: "rip", offset: 3, instrLen: 7}),
}

func makeSig(name, hexPat string, ops ...sigOp) sigPattern {
	tokens := strings.Fields(hexPat)
	pat := make([]byte, len(tokens))
	mask := make([]byte, len(tokens))
	for i, t := range tokens {
		if t == "??" {
			pat[i] = 0
			mask[i] = 0x00
		} else {
			v, err := strconv.ParseUint(t, 16, 8)
			if err != nil {
				panic(fmt.Sprintf("bad sig byte %q in pattern %s", t, name))
			}
			pat[i] = byte(v)
			mask[i] = 0xFF
		}
	}
	for i := range ops {
		if ops[i].kind == "rip" {
			if ops[i].offset == 0 {
				ops[i].offset = 3
			}
			if ops[i].instrLen == 0 {
				ops[i].instrLen = 7
			}
		}
	}
	return sigPattern{name: name, pat: pat, mask: mask, ops: ops}
}

// findPattern scans buf for the first match of pat/mask, returns offset or -1.
func findPattern(buf, pat, mask []byte) int {
	pLen := len(pat)
	if pLen == 0 || len(buf) < pLen {
		return -1
	}
	limit := len(buf) - pLen
	for i := 0; i <= limit; i++ {
		match := true
		for j := 0; j < pLen; j++ {
			if (buf[i+j] ^ pat[j]) & mask[j] != 0 {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// resolveOps applies the operation chain to produce a final uint64 value (typically an RVA).
func resolveOps(buf []byte, matchOff int, segVaddr, imageBase uint64, ops []sigOp) (uint64, error) {
	var result uint64
	for _, op := range ops {
		switch op.kind {
		case "rip":
			dispPos := matchOff + op.offset
			if dispPos+4 > len(buf) {
				return 0, fmt.Errorf("rip: disp out of range at buf[%d]", dispPos)
			}
			disp := int32(binary.LittleEndian.Uint32(buf[dispPos : dispPos+4]))
			matchVaddr := segVaddr + uint64(matchOff)
			absAddr := uint64(int64(matchVaddr) + int64(op.instrLen) + int64(disp))
			result = absAddr - imageBase
		case "add":
			result = uint64(int64(result) + op.addVal)
		case "slice":
			pos := matchOff + op.slStart
			n := op.slEnd - op.slStart
			if pos+n > len(buf) || n < 1 || n > 4 {
				return 0, fmt.Errorf("slice: out of range at buf[%d:%d]", pos, pos+n)
			}
			var v uint64
			for k := 0; k < n; k++ {
				v |= uint64(buf[pos+k]) << (8 * k)
			}
			result = v
		default:
			return 0, fmt.Errorf("unknown op %q", op.kind)
		}
	}
	return result, nil
}

// SigScanLogFunc is called for every sigscanner log line (level: "info"/"success"/"error"/"warning").
// Set from cmd/desktop to route into the web panel + file.
var SigScanLogFunc func(level, msg string)

func sigScanLog(level, msg string) {
	log.Printf("[SigScan] %s", msg)
	if fn := SigScanLogFunc; fn != nil {
		fn(level, msg)
	}
}

var sigScanCacheMu sync.Mutex
var sigScanCacheMap = map[string]*sigScanResult{}

type sigScanResult struct {
	offsets map[string]uint64
	at      time.Time
}

func sigScanCacheKey(pid int, base uint64) string {
	return fmt.Sprintf("%d:0x%x", pid, base)
}

// sigScanLibclient reads the .text (r-xp) segment(s) of libclient.so via process_vm_readv
// and scans for known byte patterns. Returns found offsets keyed by name (e.g. "dwEntityList").
func sigScanLibclient(pid int, imageBase uint64, modulePath string) (map[string]uint64, error) {
	key := sigScanCacheKey(pid, imageBase)
	sigScanCacheMu.Lock()
	if c, ok := sigScanCacheMap[key]; ok {
		sigScanCacheMu.Unlock()
		return c.offsets, nil
	}
	sigScanCacheMu.Unlock()

	t0 := time.Now()
	sigScanLog("info", fmt.Sprintf("pid=%d base=0x%x module=%q — reading .text segments...", pid, imageBase, modulePath))

	segs, err := readTextSegments(pid, modulePath)
	if err != nil {
		sigScanLog("error", fmt.Sprintf("read .text failed: %v", err))
		return nil, fmt.Errorf("sigscan read .text: %w", err)
	}
	if len(segs) == 0 {
		sigScanLog("error", fmt.Sprintf("no r-xp segments for %q", modulePath))
		return nil, fmt.Errorf("sigscan: no r-xp segments for %q", modulePath)
	}

	var totalBytes int64
	for _, s := range segs {
		totalBytes += int64(len(s.data))
	}
	sigScanLog("info", fmt.Sprintf("loaded %d .text segment(s), %.1f MB total", len(segs), float64(totalBytes)/(1024*1024)))

	result := make(map[string]uint64, len(libclientSigPatterns))
	for _, sig := range libclientSigPatterns {
		found := false
		for _, seg := range segs {
			off := findPattern(seg.data, sig.pat, sig.mask)
			if off < 0 {
				continue
			}
			val, err := resolveOps(seg.data, off, seg.vaddr, imageBase, sig.ops)
			if err != nil {
				sigScanLog("warning", fmt.Sprintf("%s: match at 0x%x but resolve failed: %v", sig.name, seg.vaddr+uint64(off), err))
				continue
			}
			result[sig.name] = val
			sigScanLog("success", fmt.Sprintf("%s = 0x%x (match at vaddr 0x%x)", sig.name, val, seg.vaddr+uint64(off)))
			found = true
			break
		}
		if !found {
			sigScanLog("warning", fmt.Sprintf("%s: pattern not found (%d bytes)", sig.name, len(sig.pat)))
		}
	}

	elapsed := time.Since(t0)
	sigScanLog("info", fmt.Sprintf("scan complete: %.1f MB in %v — found %d/%d offsets",
		float64(totalBytes)/(1024*1024), elapsed, len(result), len(libclientSigPatterns)))

	sigScanCacheMu.Lock()
	sigScanCacheMap[key] = &sigScanResult{offsets: result, at: time.Now()}
	sigScanCacheMu.Unlock()

	return result, nil
}

type textSegment struct {
	vaddr uint64
	data  []byte
}

func readTextSegments(pid int, modulePath string) ([]textSegment, error) {
	mapsRaw, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	selCore := mapsPathCore(modulePath)
	var segs []textSegment
	for _, line := range strings.Split(string(mapsRaw), "\n") {
		st, en, perms, mpath, ok := parseMapsLine(line)
		if !ok || mpath == "" || strings.HasPrefix(mpath, "[") {
			continue
		}
		if mapsPathCore(mpath) != selCore {
			continue
		}
		if len(perms) < 4 || perms[2] != 'x' {
			continue
		}
		sz := en - st
		if sz == 0 || sz > 256*1024*1024 {
			continue
		}
		buf := make([]byte, sz)
		if err := readProcMem(pid, st, buf); err != nil {
			sigScanLog("warning", fmt.Sprintf("skip segment 0x%x-0x%x: %v", st, en, err))
			continue
		}
		segs = append(segs, textSegment{vaddr: st, data: buf})
	}
	return segs, nil
}

// applySigScanToOffsets merges sigscanner results into a cs2MemoryJSON, filling only zero fields.
func applySigScanToOffsets(off *cs2MemoryJSON, found map[string]uint64) int {
	if off == nil || len(found) == 0 {
		return 0
	}
	n := 0
	set := func(dst *uint64, key string) {
		if *dst != 0 {
			return
		}
		if v, ok := found[key]; ok && v != 0 {
			*dst = v
			n++
		}
	}
	set(&off.DwLocalPlayerPawn, "dwLocalPlayerPawn")
	set(&off.DwEntityList, "dwEntityList")
	set(&off.DwLocalPlayerController, "dwLocalPlayerController")
	set(&off.DwViewMatrix, "dwViewMatrix")
	set(&off.DwGameEntitySystem, "dwGameEntitySystem")
	set(&off.DwGameEntitySystemHighestIndex, "dwGameEntitySystem_highestEntityIndex")
	return n
}

// SigScanInvalidateCache drops cached results (e.g. after game restart).
func SigScanInvalidateCache() {
	sigScanCacheMu.Lock()
	sigScanCacheMap = map[string]*sigScanResult{}
	sigScanCacheMu.Unlock()
}
