package autoplay

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const defaultYoloTCPPort = 37771

// YoloDet одна детекция в ROI (координаты в пикселях кадра w×h).
type YoloDet struct {
	Cls  string    `json:"cls"`
	Conf float64   `json:"conf"`
	Xyxy []float64 `json:"xyxy"`
}

type yoloInferJSON struct {
	Ok  bool       `json:"ok"`
	Err string     `json:"err"`
	Det []YoloDet  `json:"det"`
	Viz []YoloDet  `json:"viz"` // все классы для превью (в det только c/ch/t/th)
}

// YoloClient отправляет кадры в локальный TCP worker (yolo_worker.py).
type YoloClient struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

func NewYoloClient(addr string) *YoloClient {
	if addr == "" {
		addr = "127.0.0.1:" + strconv.Itoa(defaultYoloTCPPort)
	}
	return &YoloClient{addr: addr}
}

func (c *YoloClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *YoloClient) Infer(display int, rgb []byte, w, h int) ([]YoloDet, []YoloDet, error) {
	if w <= 0 || h <= 0 || len(rgb) != w*h*3 {
		return nil, nil, fmt.Errorf("yolo: bad frame %dx%d len=%d", w, h, len(rgb))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	if c.conn == nil {
		c.conn, err = net.DialTimeout("tcp", c.addr, 3*time.Second)
		if err != nil {
			return nil, nil, err
		}
	}
	hdr := make([]byte, 12)
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(display))
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(w))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(h))
	if _, err = writeAll(c.conn, hdr); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, nil, err
	}
	if _, err = writeAll(c.conn, rgb); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, nil, err
	}
	var nlen [4]byte
	if _, err = readFull(c.conn, nlen[:]); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, nil, err
	}
	n := int(binary.LittleEndian.Uint32(nlen[:]))
	if n <= 0 || n > 50<<20 {
		_ = c.conn.Close()
		c.conn = nil
		return nil, nil, fmt.Errorf("yolo: bad json len %d", n)
	}
	body := make([]byte, n)
	if _, err = readFull(c.conn, body); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, nil, err
	}
	var jr yoloInferJSON
	if err := json.Unmarshal(body, &jr); err != nil {
		return nil, nil, err
	}
	if !jr.Ok {
		if jr.Err != "" {
			return nil, nil, fmt.Errorf("yolo: %s", jr.Err)
		}
		return nil, nil, fmt.Errorf("yolo: inference failed")
	}
	viz := jr.Viz
	if len(viz) == 0 {
		viz = jr.Det
	}
	return jr.Det, viz, nil
}

func writeAll(c net.Conn, b []byte) (int, error) {
	off := 0
	for off < len(b) {
		n, err := c.Write(b[off:])
		if n <= 0 && err == nil {
			return off, fmt.Errorf("yolo: short write")
		}
		if err != nil {
			return off, err
		}
		off += n
	}
	return off, nil
}

func readFull(c net.Conn, b []byte) (int, error) {
	off := 0
	for off < len(b) {
		n, err := c.Read(b[off:])
		if n <= 0 && err == nil {
			return off, fmt.Errorf("yolo: short read")
		}
		if err != nil {
			return off, err
		}
		off += n
	}
	return off, nil
}

// FindRepoRoot ищет корень репозитория (папка с go.mod).
func FindRepoRoot(start string) string {
	dir := start
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			break
		}
		dir = p
	}
	return ""
}

func resolveYoloPython(repoRoot string) string {
	if p := os.Getenv("SFARM_YOLO_PYTHON"); p != "" {
		return p
	}
	if repoRoot != "" {
		vpy := filepath.Join(repoRoot, "tools/cs2-yolo/.venv/bin/python")
		if st, err := os.Stat(vpy); err == nil && !st.IsDir() {
			return vpy
		}
	}
	return "python3"
}

func resolveYoloWeights(repoRoot string) string {
	if p := os.Getenv("SFARM_YOLO_WEIGHTS"); p != "" {
		if isUsableWeightsFile(p) {
			return p
		}
		log.Printf("[Autoplay] YOLO: SFARM_YOLO_WEIGHTS is missing or Git LFS stub: %s", p)
	}
	if repoRoot == "" {
		return ""
	}
	cands := []string{
		filepath.Join(repoRoot, "ai-bot/yolov8/cs2_yolov8n_640.pt"),
		filepath.Join(repoRoot, "ai-bot/yolov8/yolov8n.pt"),
		filepath.Join(repoRoot, "ai-bot/yolov8/cs2_yolov8s_640.pt"),
		filepath.Join(repoRoot, "ai-bot/yolov8/yolov8s_csgoV1_640.pt"),
	}
	for _, p := range cands {
		if isUsableWeightsFile(p) {
			return p
		}
	}
	return ""
}

// isUsableWeightsFile отсекает отсутствующие файлы и Git LFS pointer (не скачанный настоящий .pt).
func isUsableWeightsFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	if st.Size() < 64*1024 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 48)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false
	}
	if n < 20 {
		return false
	}
	if bytes.HasPrefix(buf[:n], []byte("version https://git-lfs")) {
		return false
	}
	return true
}

// YoloWorkerProcess результат StartYoloWorker: клиент и процесс (для Kill при shutdown).
type YoloWorkerProcess struct {
	Client *YoloClient
	Cmd    *exec.Cmd
}

const yoloTCPHost = "127.0.0.1"

// pickYoloWorkerPort резервирует порт до старта Python, чтобы не ловить EADDRINUSE после краша
// старого воркера на 37771. По умолчанию — свободный эфемерный порт. SFARM_YOLO_PORT — жёстко, с проверкой.
func pickYoloWorkerPort() (int, error) {
	if p := os.Getenv("SFARM_YOLO_PORT"); p != "" {
		v, err := strconv.Atoi(p)
		if err != nil || v <= 0 || v >= 65536 {
			return 0, fmt.Errorf("SFARM_YOLO_PORT=%q некорректен", p)
		}
		addr := fmt.Sprintf("%s:%d", yoloTCPHost, v)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return 0, fmt.Errorf("порт YOLO %s занят (остановите старый процесс или уберите SFARM_YOLO_PORT для автопорта): %w", addr, err)
		}
		_ = ln.Close()
		return v, nil
	}
	ln, err := net.Listen("tcp", yoloTCPHost+":0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// StartYoloWorker запускает yolo_worker.py если не задано SFARM_YOLO_DISABLED=1 и найдены веса.
// logOut получает объединённый stdout/stderr воркера (например для Web UI); nil — в консоль процесса.
// Процесс не привязан к контексту приложения — завершается в Manager.Shutdown.
func StartYoloWorker(logOut io.Writer) (*YoloWorkerProcess, error) {
	if os.Getenv("SFARM_YOLO_DISABLED") == "1" {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	root := FindRepoRoot(filepath.Dir(exe))
	if root == "" {
		if wd, werr := os.Getwd(); werr == nil {
			root = FindRepoRoot(wd)
		}
	}
	wpath := resolveYoloWeights(root)
	if wpath == "" {
		log.Printf("[Autoplay] YOLO: no usable .pt (Git LFS stubs are skipped — run «git lfs pull» или задайте SFARM_YOLO_WEIGHTS)")
		return nil, nil
	}
	script := filepath.Join(root, "tools/cs2-yolo/yolo_worker.py")
	if st, err := os.Stat(script); err != nil || st.IsDir() {
		log.Printf("[Autoplay] YOLO: worker script missing: %s", script)
		return nil, nil
	}
	port, err := pickYoloWorkerPort()
	if err != nil {
		return nil, err
	}
	python := resolveYoloPython(root)
	cmd := exec.Command(python, script, "--weights", wpath, "--port", strconv.Itoa(port))
	cmd.Dir = root
	if logOut != nil {
		cmd.Stdout = logOut
		cmd.Stderr = logOut
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("yolo worker: %w", err)
	}
	addr := yoloTCPHost + ":" + strconv.Itoa(port)
	log.Printf("[Autoplay] YOLO worker started: %s addr=%s", wpath, addr)
	time.Sleep(300 * time.Millisecond)
	return &YoloWorkerProcess{
		Client: NewYoloClient(addr),
		Cmd:    cmd,
	}, nil
}

var dmHeadClasses = map[string]bool{"ch": true, "th": true}

// PickDMBest выбирает цель для deathmatch: все c,ch,t,th; приоритет голове и уверенности.
func PickDMBestTarget(dets []YoloDet, frameW, frameH int) (best *YoloDet, aimX, aimY, errPx float64) {
	if len(dets) == 0 {
		return nil, 0, 0, 0
	}
	cx := float64(frameW) * 0.5
	cy := float64(frameH) * 0.5
	const maxR = 360.0
	bestScore := -1e9
	var chosen *YoloDet
	for i := range dets {
		d := &dets[i]
		if len(d.Xyxy) < 4 {
			continue
		}
		x1, y1, x2, y2 := d.Xyxy[0], d.Xyxy[1], d.Xyxy[2], d.Xyxy[3]
		ax, ay := yoloAimPoint(x1, y1, x2, y2, d.Cls)
		dx, dy := ax-cx, ay-cy
		dist := math.Hypot(dx, dy)
		if dist > maxR {
			continue
		}
		headBoost := 0.0
		if dmHeadClasses[d.Cls] {
			headBoost = 55
		}
		score := d.Conf*220 - dist*0.85 + headBoost
		if score > bestScore {
			bestScore = score
			chosen = d
			aimX, aimY = ax, ay
		}
	}
	if chosen == nil {
		return nil, 0, 0, 0
	}
	errPx = math.Hypot(aimX-cx, aimY-cy)
	return chosen, aimX, aimY, errPx
}

func yoloAimPoint(x1, y1, x2, y2 float64, cls string) (float64, float64) {
	if dmHeadClasses[cls] {
		return (x1 + x2) * 0.5, (y1 + y2) * 0.5
	}
	return (x1 + x2) * 0.5, y1 + (y2-y1)*0.34
}

// SmoothErrPxAlpha сглаживание ошибки прицела (для delay/lag стрельбы).
func SmoothErrPxAlpha(cur, target float64, alpha float64) float64 {
	return cur*(1-alpha) + target*alpha
}

// YoloEmaBlend экспоненциальное сглаживание прицела (меньше alpha — плавнее).
func YoloEmaBlend(prev, sample, alpha float64) float64 {
	return prev*(1-alpha) + sample*alpha
}

// YoloROIGame640 — центральный квадрат 640×640 под YOLOv8n@640.
func YoloROIGame640() (x0, y0, rw, rh int) {
	const side = 640
	cx := 512 * gameClientW / menuRefW
	cy := 336 * gameClientH / menuRefH
	x0 = cx - side/2
	y0 = cy - side/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	return x0, y0, side, side
}

// NormalizeGrabbedRGB X11 часто подрезает ROI у края окна (меньше запрошенного w×h), буфер = фактические пиксели.
// Возвращает реальные w,h для передачи в YOLO и PickDMBestTarget.
func NormalizeGrabbedRGB(rgb []byte, reqW, reqH int) (w, h int, ok bool) {
	n := len(rgb)
	if n < 24 || n%3 != 0 {
		return 0, 0, false
	}
	if reqW > 0 && reqH > 0 && n == reqW*reqH*3 {
		return reqW, reqH, true
	}
	if reqW > 0 && n%(reqW*3) == 0 {
		h2 := n / (reqW * 3)
		if h2 >= 8 {
			return reqW, h2, true
		}
	}
	if reqH > 0 && n%(reqH*3) == 0 {
		w2 := n / (reqH * 3)
		if w2 >= 8 {
			return w2, reqH, true
		}
	}
	return 0, 0, false
}

