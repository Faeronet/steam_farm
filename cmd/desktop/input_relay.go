package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type InputRelay struct {
	upgrader websocket.Upgrader
	mu       sync.Mutex
	procs    map[int]*relayProc // display -> process
}

type relayProc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
}

func NewInputRelay() *InputRelay {
	return &InputRelay{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		procs: make(map[int]*relayProc),
	}
}

func (ir *InputRelay) getOrStartRelay(display int) (*relayProc, error) {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	if rp, ok := ir.procs[display]; ok {
		if rp.cmd.Process != nil {
			return rp, nil
		}
		delete(ir.procs, display)
	}

	binDir := filepath.Dir(mustSelfExe())
	relayBin := filepath.Join(binDir, "xinput_relay")

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
		return nil, fmt.Errorf("failed to start xinput_relay: %w", err)
	}

	reader := bufio.NewReader(stdout)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "READY") {
		stdin.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("xinput_relay did not report READY")
	}

	rp := &relayProc{cmd: cmd, stdin: stdin}
	ir.procs[display] = rp
	log.Printf("[InputRelay] Started for display :%d (pid %d)", display, cmd.Process.Pid)

	go func() {
		cmd.Wait()
		ir.mu.Lock()
		delete(ir.procs, display)
		ir.mu.Unlock()
		log.Printf("[InputRelay] Process for display :%d exited", display)
	}()

	return rp, nil
}

func (rp *relayProc) send(cmd string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	fmt.Fprintln(rp.stdin, cmd)
}

func mustSelfExe() string {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	abs, _ := filepath.Abs(exe)
	return abs
}

// Handle WebSocket: /ws/input/{display}
// Messages: JSON {"t":"m","dx":5,"dy":-3} for mouse move
//           {"t":"bd","b":1} for button down
//           {"t":"bu","b":1} for button up
func (ir *InputRelay) Handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/ws/input/"), "/")
	if len(parts) == 0 {
		http.Error(w, "missing display", http.StatusBadRequest)
		return
	}
	display, err := strconv.Atoi(parts[0])
	if err != nil || display < 1 || display > 200 {
		http.Error(w, "invalid display", http.StatusBadRequest)
		return
	}

	rp, err := ir.getOrStartRelay(display)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	conn, err := ir.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[InputRelay] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[InputRelay] Client connected for display :%d", display)

	for {
		var msg struct {
			T  string `json:"t"`
			DX int    `json:"dx,omitempty"`
			DY int    `json:"dy,omitempty"`
			B  int    `json:"b,omitempty"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[InputRelay] Read error: %v", err)
			}
			return
		}

		switch msg.T {
		case "m":
			rp.send(fmt.Sprintf("M %d %d", msg.DX, msg.DY))
		case "bd":
			rp.send(fmt.Sprintf("B %d", msg.B))
		case "bu":
			rp.send(fmt.Sprintf("b %d", msg.B))
		}
	}
}

func (ir *InputRelay) Shutdown() {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	for d, rp := range ir.procs {
		rp.send("Q")
		rp.stdin.Close()
		rp.cmd.Process.Kill()
		delete(ir.procs, d)
	}
}
