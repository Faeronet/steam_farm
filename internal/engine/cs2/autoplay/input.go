package autoplay

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// X11 keysyms
const (
	KeyW      = 0x0077
	KeyA      = 0x0061
	KeyS      = 0x0073
	KeyD      = 0x0064
	KeyR      = 0x0072
	KeyE      = 0x0065
	KeySpace  = 0x0020
	KeyEscape = 0xff1b
	KeyReturn = 0xff0d
	KeyTab    = 0xff09
	Key1      = 0x0031
	Key2      = 0x0032
	Key3      = 0x0033
	Key4      = 0x0034
	Key5      = 0x0035
	KeyShiftL = 0xffe1
	KeyCtrlL  = 0xffe3
	KeyF1     = 0xffbe
	KeyTilde  = 0x0060 // console toggle (`)
)

type InputSender struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	done  chan struct{}
}

func NewInputSender(display int) (*InputSender, error) {
	relayBin, err := findRelayBin()
	if err != nil {
		return nil, err
	}

	displayStr := fmt.Sprintf(":%d", display)
	cmd := exec.Command(relayBin, displayStr)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start xinput_relay: %w", err)
	}

	reader := bufio.NewReader(stdout)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "READY") {
		stdin.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("xinput_relay did not report READY")
	}

	s := &InputSender{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

func (s *InputSender) MouseMove(dx, dy int) {
	s.send(fmt.Sprintf("M %d %d", dx, dy))
}

func (s *InputSender) WarpAbsolute(x, y int) {
	s.send(fmt.Sprintf("W %d %d", x, y))
}

func (s *InputSender) MouseDown(button int) {
	s.send(fmt.Sprintf("B %d", button))
}

func (s *InputSender) MouseUp(button int) {
	s.send(fmt.Sprintf("b %d", button))
}

func (s *InputSender) KeyDown(keysym uint) {
	s.send(fmt.Sprintf("K %d", keysym))
}

func (s *InputSender) KeyUp(keysym uint) {
	s.send(fmt.Sprintf("k %d", keysym))
}

func (s *InputSender) KeyTap(keysym uint) {
	s.KeyDown(keysym)
	s.KeyUp(keysym)
}

func (s *InputSender) Click(button int) {
	s.MouseDown(button)
	s.MouseUp(button)
}

func (s *InputSender) Close() {
	s.send("Q")
	s.stdin.Close()
	<-s.done
}

func (s *InputSender) send(cmd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(s.stdin, cmd)
}

func findRelayBin() (string, error) {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "xinput_relay"),
		"bin/xinput_relay",
		"xinput_relay",
	}
	for _, p := range candidates {
		abs, _ := filepath.Abs(p)
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf("xinput_relay binary not found")
}
