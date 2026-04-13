package autoplay

/*
#cgo LDFLAGS: -lX11 -lXtst -lpthread
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/extensions/XTest.h>
#include <X11/Xatom.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <stdio.h>
#include <pthread.h>

static volatile int x11_broken = 0;

static int x11_io_error_handler(Display* d) {
	x11_broken = 1;
	pthread_exit(NULL);
	return 0;
}

static int x11_nonfatal_error_handler(Display* d, XErrorEvent* e) {
	return 0;
}

static void x11_install_handlers() {
	XSetIOErrorHandler(x11_io_error_handler);
	XSetErrorHandler(x11_nonfatal_error_handler);
}

static int x11_is_broken() { return x11_broken; }
static void x11_clear_broken() { x11_broken = 0; }

static Display* x11_open(const char* name) {
	x11_install_handlers();
	x11_clear_broken();
	return XOpenDisplay(name);
}

static void x11_close(Display* d) {
	if (d) XCloseDisplay(d);
}

static int x11_key_down(Display* d, unsigned long keysym) {
	KeyCode kc = XKeysymToKeycode(d, (KeySym)keysym);
	if (!kc) return -1;
	XTestFakeKeyEvent(d, kc, True, CurrentTime);
	XFlush(d);
	return 0;
}

static int x11_key_up(Display* d, unsigned long keysym) {
	KeyCode kc = XKeysymToKeycode(d, (KeySym)keysym);
	if (!kc) return -1;
	XTestFakeKeyEvent(d, kc, False, CurrentTime);
	XFlush(d);
	return 0;
}

static void x11_btn_down(Display* d, unsigned int button) {
	XTestFakeButtonEvent(d, button, True, CurrentTime);
	XFlush(d);
}

static void x11_btn_up(Display* d, unsigned int button) {
	XTestFakeButtonEvent(d, button, False, CurrentTime);
	XFlush(d);
}

// Full click with 80ms hold between press and release
static void x11_click_at(Display* d, int x, int y, unsigned int button) {
	int screen = DefaultScreen(d);
	XTestFakeMotionEvent(d, screen, x, y, CurrentTime);
	XFlush(d);
	usleep(30000);
	XTestFakeButtonEvent(d, button, True, CurrentTime);
	XFlush(d);
	usleep(80000);
	XTestFakeButtonEvent(d, button, False, CurrentTime);
	XFlush(d);
}

static void x11_mouse_rel(Display* d, int dx, int dy) {
	XTestFakeRelativeMotionEvent(d, dx, dy, CurrentTime);
	XFlush(d);
}

static void x11_mouse_abs(Display* d, int x, int y) {
	int screen = DefaultScreen(d);
	XTestFakeMotionEvent(d, screen, x, y, CurrentTime);
	XFlush(d);
}

// Cached CS2 window — avoids searching every frame
static Window cs2_cached_window = 0;

static int name_matches_cs2(const char* name) {
	if (!name) return 0;
	if (strstr(name, "Counter-Strike")) return 1;
	if (strstr(name, "counter-strike")) return 1;
	if (strstr(name, "CS2") || strstr(name, "cs2")) return 1;
	return 0;
}

// Match WM_NAME, WM_CLASS, and EWMH _NET_WM_NAME (SDL/Source2 often nest below root).
static int window_matches_cs2(Display* d, Window w) {
	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, w, &attr) || attr.map_state != IsViewable)
		return 0;

	char* wm_name = NULL;
	if (XFetchName(d, w, &wm_name) && wm_name) {
		int m = name_matches_cs2(wm_name);
		XFree(wm_name);
		if (m) return 1;
	}

	XClassHint class_hint;
	memset(&class_hint, 0, sizeof(class_hint));
	if (XGetClassHint(d, w, &class_hint)) {
		int m = name_matches_cs2(class_hint.res_name) ||
		        name_matches_cs2(class_hint.res_class);
		if (class_hint.res_name)  XFree(class_hint.res_name);
		if (class_hint.res_class) XFree(class_hint.res_class);
		if (m) return 1;
	}

	Atom utf8 = XInternAtom(d, "UTF8_STRING", False);
	Atom net_wm_name = XInternAtom(d, "_NET_WM_NAME", False);
	Atom act_type;
	int act_fmt;
	unsigned long nitems, bytes_after;
	unsigned char* prop = NULL;
	int gp = XGetWindowProperty(d, w, net_wm_name, 0, 1024, False, utf8,
	    &act_type, &act_fmt, &nitems, &bytes_after, &prop);
	if (gp == Success && prop) {
		if (act_fmt == 8 && nitems > 0 && name_matches_cs2((char*)prop)) {
			XFree(prop);
			return 1;
		}
		XFree(prop);
	}
	return 0;
}

static Window x11_find_cs2_recursive(Display* d, Window w, int depth, int max_depth) {
	if (depth > max_depth) return 0;
	if (window_matches_cs2(d, w)) return w;

	Window root_ret, parent_ret;
	Window* children = NULL;
	unsigned int nchildren = 0;
	if (!XQueryTree(d, w, &root_ret, &parent_ret, &children, &nchildren))
		return 0;

	Window found = 0;
	for (unsigned int i = 0; i < nchildren && !found; i++) {
		found = x11_find_cs2_recursive(d, children[i], depth + 1, max_depth);
	}
	if (children) XFree(children);
	return found;
}

// Strict name/class match only; full tree walk (no "largest window" fallback).
static Window x11_find_cs2(Display* d) {
	return x11_find_cs2_recursive(d, DefaultRootWindow(d), 0, 24);
}

// Focus the CS2 window. Uses cached window ID if still valid.
static int x11_focus_game(Display* d) {
	// Validate cached window
	if (cs2_cached_window) {
		XWindowAttributes attr;
		if (!XGetWindowAttributes(d, cs2_cached_window, &attr) ||
		    attr.map_state != IsViewable) {
			cs2_cached_window = 0;
		}
	}

	if (!cs2_cached_window) {
		cs2_cached_window = x11_find_cs2(d);
	}

	if (!cs2_cached_window) return -1;

	XSetInputFocus(d, cs2_cached_window, RevertToParent, CurrentTime);
	XRaiseWindow(d, cs2_cached_window);
	XFlush(d);
	return 0;
}

// Diagnostic: list all visible windows with their names and sizes.
// Returns number of windows found. Writes info to stderr for Go to capture.
static int x11_list_windows(Display* d) {
	Window root = DefaultRootWindow(d);
	Window parent_ret;
	Window* children = NULL;
	unsigned int nchildren = 0;
	int count = 0;

	if (!XQueryTree(d, root, &root, &parent_ret, &children, &nchildren))
		return 0;

	for (unsigned int i = 0; i < nchildren; i++) {
		XWindowAttributes attr;
		if (!XGetWindowAttributes(d, children[i], &attr) || attr.map_state != IsViewable)
			continue;

		char* wm_name = NULL;
		char class_name[256] = "(none)";

		XFetchName(d, children[i], &wm_name);

		XClassHint class_hint;
		memset(&class_hint, 0, sizeof(class_hint));
		if (XGetClassHint(d, children[i], &class_hint)) {
			snprintf(class_name, sizeof(class_name), "%s/%s",
				class_hint.res_name ? class_hint.res_name : "?",
				class_hint.res_class ? class_hint.res_class : "?");
			if (class_hint.res_name)  XFree(class_hint.res_name);
			if (class_hint.res_class) XFree(class_hint.res_class);
		}

		fprintf(stderr, "[X11Diag] Window 0x%lx: %dx%d name=\"%s\" class=\"%s\"\n",
			(unsigned long)children[i], attr.width, attr.height,
			wm_name ? wm_name : "(none)", class_name);

		if (wm_name) XFree(wm_name);
		count++;
	}

	if (children) XFree(children);
	return count;
}

static void x11_invalidate_cache() {
	cs2_cached_window = 0;
}

static int x11_get_keycode(Display* d, unsigned long keysym) {
	return (int)XKeysymToKeycode(d, (KeySym)keysym);
}

// Type a single character with automatic Shift handling for chars that need it.
static void x11_type_char(Display* d, char c) {
	KeySym ks = 0;
	int shift = 0;

	if (c >= 'a' && c <= 'z')      { ks = (KeySym)c; }
	else if (c >= 'A' && c <= 'Z') { ks = (KeySym)(c + 32); shift = 1; } // lowercase keysym + shift
	else if (c >= '0' && c <= '9') { ks = (KeySym)c; }
	else if (c == ' ')  { ks = 0x0020; }
	else if (c == '_')  { ks = 0x005f; shift = 1; }
	else if (c == ';')  { ks = 0x003b; }
	else if (c == '.')  { ks = 0x002e; }
	else if (c == ',')  { ks = 0x002c; }
	else if (c == '+')  { ks = 0x002b; shift = 1; }
	else if (c == '-')  { ks = 0x002d; }
	else if (c == '/')  { ks = 0x002f; }
	else if (c == '\n') { ks = 0xff0d; }
	else { return; }

	KeyCode kc = XKeysymToKeycode(d, ks);
	if (!kc) return;

	if (shift) {
		KeyCode skc = XKeysymToKeycode(d, 0xffe1); // Shift_L
		XTestFakeKeyEvent(d, skc, True, CurrentTime);
	}
	XTestFakeKeyEvent(d, kc, True, CurrentTime);
	XTestFakeKeyEvent(d, kc, False, CurrentTime);
	if (shift) {
		KeyCode skc = XKeysymToKeycode(d, 0xffe1);
		XTestFakeKeyEvent(d, skc, False, CurrentTime);
	}
	XFlush(d);
	usleep(8000); // 8ms per char — fast enough but reliable for game console
}

// Type a null-terminated string, then press Enter.
static void x11_type_line(Display* d, const char* text) {
	for (int i = 0; text[i]; i++) {
		x11_type_char(d, text[i]);
	}
	// Press Enter
	KeyCode kc = XKeysymToKeycode(d, 0xff0d);
	if (kc) {
		XTestFakeKeyEvent(d, kc, True, CurrentTime);
		XTestFakeKeyEvent(d, kc, False, CurrentTime);
		XFlush(d);
	}
}

// Grayscale rectangle from CS2 client (ZPixmap 24/32 bpp). Caller frees *out with free().
static int x11_grab_cs2_gray_rect(Display* d, int x0, int y0, int roiW, int roiH, unsigned char** out, int* outLen) {
	if (!out || !outLen) return -1;
	*out = NULL;
	*outLen = 0;

	if (!cs2_cached_window)
		cs2_cached_window = x11_find_cs2(d);
	if (!cs2_cached_window)
		return -2;

	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, cs2_cached_window, &attr) || attr.map_state != IsViewable)
		return -3;

	int winW = attr.width;
	int winH = attr.height;
	if (roiW < 8 || roiH < 8)
		return -4;
	if (x0 < 0) x0 = 0;
	if (y0 < 0) y0 = 0;
	if (x0 + roiW > winW) roiW = winW - x0;
	if (y0 + roiH > winH) roiH = winH - y0;
	if (roiW < 8 || roiH < 8)
		return -4;

	XImage* img = XGetImage(d, cs2_cached_window, x0, y0, (unsigned)roiW, (unsigned)roiH,
		AllPlanes, ZPixmap);
	if (!img)
		return -5;

	int bps = img->bits_per_pixel;
	int bpl = img->bytes_per_line;
	char* raw = img->data;
	if (!raw || (bps != 32 && bps != 24)) {
		XDestroyImage(img);
		return -6;
	}

	int bpp = bps / 8;
	int n = roiW * roiH;
	unsigned char* buf = (unsigned char*)malloc((size_t)n);
	if (!buf) {
		XDestroyImage(img);
		return -7;
	}

	for (int y = 0; y < roiH; y++) {
		unsigned char* row = (unsigned char*)(raw + y * bpl);
		for (int x = 0; x < roiW; x++) {
			unsigned char* p = row + x * bpp;
			unsigned char bb = p[0], g = p[1], r = p[2];
			// BT.601 luma — лучше отделяет цвета HUD/миникарты, чем простое среднее BGR.
			int yv = ((int)r * 77 + (int)g * 150 + (int)bb * 29 + 128) >> 8;
			if (yv > 255) yv = 255;
			buf[y * roiW + x] = (unsigned char)yv;
		}
	}

	XDestroyImage(img);
	*out = buf;
	*outLen = n;
	return 0;
}

static int x11_grab_cs2_gray(Display* d, int roiW, int roiH, unsigned char** out, int* outLen) {
	if (!cs2_cached_window)
		cs2_cached_window = x11_find_cs2(d);
	if (!cs2_cached_window)
		return -2;
	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, cs2_cached_window, &attr) || attr.map_state != IsViewable)
		return -3;
	int winW = attr.width;
	int winH = attr.height;
	if (roiW < 8 || roiH < 8 || winW < roiW + 4 || winH < roiH + 4)
		return -4;
	int x0 = (winW - roiW) / 2;
	int y0 = (winH - roiH) / 2;
	return x11_grab_cs2_gray_rect(d, x0, y0, roiW, roiH, out, outLen);
}

// Interleaved RGB (3 * roiW * roiH). Caller frees *out with free().
static int x11_grab_cs2_rgb_rect(Display* d, int x0, int y0, int roiW, int roiH, unsigned char** out, int* outLen) {
	if (!out || !outLen) return -1;
	*out = NULL;
	*outLen = 0;

	if (!cs2_cached_window)
		cs2_cached_window = x11_find_cs2(d);
	if (!cs2_cached_window)
		return -2;

	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, cs2_cached_window, &attr) || attr.map_state != IsViewable)
		return -3;

	int winW = attr.width;
	int winH = attr.height;
	if (roiW < 8 || roiH < 8)
		return -4;
	if (x0 < 0) x0 = 0;
	if (y0 < 0) y0 = 0;
	if (x0 + roiW > winW) roiW = winW - x0;
	if (y0 + roiH > winH) roiH = winH - y0;
	if (roiW < 8 || roiH < 8)
		return -4;

	XImage* img = XGetImage(d, cs2_cached_window, x0, y0, (unsigned)roiW, (unsigned)roiH,
		AllPlanes, ZPixmap);
	if (!img)
		return -5;

	int bps = img->bits_per_pixel;
	int bpl = img->bytes_per_line;
	char* raw = img->data;
	if (!raw || (bps != 32 && bps != 24)) {
		XDestroyImage(img);
		return -6;
	}

	int bpp = bps / 8;
	int n = roiW * roiH * 3;
	unsigned char* buf = (unsigned char*)malloc((size_t)n);
	if (!buf) {
		XDestroyImage(img);
		return -7;
	}

	for (int y = 0; y < roiH; y++) {
		unsigned char* row = (unsigned char*)(raw + y * bpl);
		for (int x = 0; x < roiW; x++) {
			unsigned char* p = row + x * bpp;
			int di = (y * roiW + x) * 3;
			buf[di + 0] = p[0];
			buf[di + 1] = p[1];
			buf[di + 2] = p[2];
		}
	}

	XDestroyImage(img);
	*out = buf;
	*outLen = n;
	return 0;
}

// Размер клиентской области окна CS2 (как у XGetImage) — для W2S/оверлея ESP в пикселях реального разрешения.
static int x11_cs2_client_size(Display* d, int* out_w, int* out_h) {
	if (!out_w || !out_h) return -1;
	if (!cs2_cached_window)
		cs2_cached_window = x11_find_cs2(d);
	if (!cs2_cached_window)
		return -2;
	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, cs2_cached_window, &attr) || attr.map_state != IsViewable)
		return -3;
	*out_w = attr.width;
	*out_h = attr.height;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

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
	KeyTilde  = 0x0060
)

type cmdKind int

const (
	cmdKeyDown cmdKind = iota
	cmdKeyUp
	cmdBtnDown
	cmdBtnUp
	cmdMouseRel
	cmdMouseAbs
	cmdFocus
	cmdListWindows
	cmdInvalidateCache
	cmdTypeLine // a = pointer to C string (passed via typeLineStr)
	cmdClickAt  // a=x, b=y, c=button — atomic move+click with hold delay
	cmdHasCS2
	cmdPing
	cmdGrabGray
	cmdGrabGrayRect // a=x0 b=y0 c=w d=h → grabGrayChan
	cmdGrabRGBRect  // a=x0 b=y0 c=w d=h → grabRGBChan (RGB interleaved, len w*h*3)
	cmdCS2ClientSize
	cmdQuit
)

type x11Cmd struct {
	kind    cmdKind
	a, b, c int
	d       int
	str     string // for cmdTypeLine
}

type InputSender struct {
	display      int
	cmds         chan x11Cmd
	pong         chan struct{}
	hasCS2Chan   chan bool
	grabGrayChan chan []byte
	grabRGBChan  chan []byte
	cs2SizeChan  chan [2]int
	done         chan struct{}
	mu           sync.Mutex
	closed       bool
	alive        atomic.Bool
	workerGen    atomic.Int64
}

func NewInputSender(display int) (*InputSender, error) {
	s := &InputSender{
		display:      display,
		cmds:         make(chan x11Cmd, 512),
		pong:         make(chan struct{}, 1),
		hasCS2Chan:   make(chan bool, 1),
		grabGrayChan: make(chan []byte, 2),
		grabRGBChan:  make(chan []byte, 2),
		cs2SizeChan:  make(chan [2]int, 1),
		done:         make(chan struct{}),
	}

	const maxAttempts = 30
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := s.startWorker()
		if err == nil {
			go s.watchdog()
			return s, nil
		}
		if attempt < maxAttempts {
			log.Printf("[X11Input] Display :%d not ready (attempt %d/%d): %v", display, attempt, maxAttempts, err)
			time.Sleep(2 * time.Second)
		} else {
			return nil, fmt.Errorf("X11 display :%d not ready after %d attempts: %w", display, maxAttempts, err)
		}
	}
	return nil, fmt.Errorf("unreachable")
}

func (s *InputSender) startWorker() error {
	ready := make(chan error, 1)
	gen := s.workerGen.Add(1)
	go s.worker(ready, gen)
	return <-ready
}

func (s *InputSender) worker(ready chan<- error, gen int64) {
	runtime.LockOSThread()

	dispStr := fmt.Sprintf(":%d", s.display)
	cstr := C.CString(dispStr)
	defer C.free(unsafe.Pointer(cstr))

	dpy := C.x11_open(cstr)
	if dpy == nil {
		ready <- fmt.Errorf("cannot open X11 display %s", dispStr)
		return
	}

	importantKeys := []struct {
		name string
		sym  uint
	}{
		{"W", KeyW}, {"A", KeyA}, {"S", KeyS}, {"D", KeyD},
		{"Space", KeySpace}, {"Return", KeyReturn}, {"Escape", KeyEscape},
	}
	for _, k := range importantKeys {
		kc := C.x11_get_keycode(dpy, C.ulong(k.sym))
		if kc == 0 {
			log.Printf("[X11Input] WARNING: keysym %s (0x%04x) has no keycode!", k.name, k.sym)
		}
	}

	log.Printf("[X11Input] Connected to display %s (worker gen=%d)", dispStr, gen)
	s.alive.Store(true)
	ready <- nil

	for cmd := range s.cmds {
		if s.workerGen.Load() != gen {
			log.Printf("[X11Input] Worker gen=%d superseded, exiting", gen)
			C.x11_close(dpy)
			return
		}

		switch cmd.kind {
		case cmdKeyDown:
			C.x11_key_down(dpy, C.ulong(cmd.a))
		case cmdKeyUp:
			C.x11_key_up(dpy, C.ulong(cmd.a))
		case cmdBtnDown:
			C.x11_btn_down(dpy, C.uint(cmd.a))
		case cmdBtnUp:
			C.x11_btn_up(dpy, C.uint(cmd.a))
		case cmdMouseRel:
			C.x11_mouse_rel(dpy, C.int(cmd.a), C.int(cmd.b))
		case cmdMouseAbs:
			C.x11_mouse_abs(dpy, C.int(cmd.a), C.int(cmd.b))
		case cmdFocus:
			C.x11_focus_game(dpy)
		case cmdListWindows:
			C.x11_list_windows(dpy)
		case cmdInvalidateCache:
			C.x11_invalidate_cache()
		case cmdTypeLine:
			cstr := C.CString(cmd.str)
			C.x11_type_line(dpy, cstr)
			C.free(unsafe.Pointer(cstr))
		case cmdClickAt:
			C.x11_click_at(dpy, C.int(cmd.a), C.int(cmd.b), C.uint(cmd.c))
		case cmdHasCS2:
			C.x11_invalidate_cache()
			found := C.x11_find_cs2(dpy) != 0
			select {
			case s.hasCS2Chan <- found:
			default:
			}
		case cmdPing:
			select {
			case s.pong <- struct{}{}:
			default:
			}
		case cmdGrabGray:
			var cbuf *C.uchar
			var cn C.int
			rc := C.x11_grab_cs2_gray(dpy, C.int(cmd.a), C.int(cmd.b), &cbuf, &cn)
			var slice []byte
			if rc == 0 && cn > 0 && cbuf != nil {
				slice = C.GoBytes(unsafe.Pointer(cbuf), cn)
				C.free(unsafe.Pointer(cbuf))
			}
			s.grabGrayChan <- slice
		case cmdGrabGrayRect:
			var cbuf *C.uchar
			var cn C.int
			rc := C.x11_grab_cs2_gray_rect(dpy, C.int(cmd.a), C.int(cmd.b), C.int(cmd.c), C.int(cmd.d), &cbuf, &cn)
			var slice []byte
			if rc == 0 && cn > 0 && cbuf != nil {
				slice = C.GoBytes(unsafe.Pointer(cbuf), cn)
				C.free(unsafe.Pointer(cbuf))
			}
			s.grabGrayChan <- slice
		case cmdGrabRGBRect:
			var cbuf *C.uchar
			var cn C.int
			rc := C.x11_grab_cs2_rgb_rect(dpy, C.int(cmd.a), C.int(cmd.b), C.int(cmd.c), C.int(cmd.d), &cbuf, &cn)
			var slice []byte
			if rc == 0 && cn > 0 && cbuf != nil {
				slice = C.GoBytes(unsafe.Pointer(cbuf), cn)
				C.free(unsafe.Pointer(cbuf))
			}
			s.grabRGBChan <- slice
		case cmdCS2ClientSize:
			var cw, ch C.int
			rc := C.x11_cs2_client_size(dpy, &cw, &ch)
			var sz [2]int
			if rc == 0 {
				sz[0], sz[1] = int(cw), int(ch)
			}
			select {
			case s.cs2SizeChan <- sz:
			default:
			}
		case cmdQuit:
			C.x11_close(dpy)
			s.alive.Store(false)
			close(s.done)
			return
		}
	}
	C.x11_close(dpy)
}

// watchdog detects when the worker thread dies (X connection broken)
// and spawns a new worker.
func (s *InputSender) watchdog() {
	for {
		time.Sleep(10 * time.Second)

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		if C.x11_is_broken() != 0 {
			log.Printf("[X11Input] Detected broken X connection — reconnecting")
			s.alive.Store(false)

			// Drain any pending commands
			for {
				select {
				case <-s.cmds:
				default:
					goto drained
				}
			}
		drained:

			s.cmds = make(chan x11Cmd, 512)
			C.x11_clear_broken()

			for attempt := 1; attempt <= 30; attempt++ {
				time.Sleep(2 * time.Second)
				if err := s.startWorker(); err == nil {
					log.Printf("[X11Input] Reconnected after %d attempts", attempt)
					break
				}
				if attempt%5 == 0 {
					log.Printf("[X11Input] Reconnect attempt %d/30 failed", attempt)
				}
			}
		}
	}
}

func (s *InputSender) send(cmd x11Cmd) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	if !s.alive.Load() {
		return
	}

	select {
	case s.cmds <- cmd:
	default:
	}
}

func (s *InputSender) KeyDown(keysym uint)   { s.send(x11Cmd{kind: cmdKeyDown, a: int(keysym)}) }
func (s *InputSender) KeyUp(keysym uint)     { s.send(x11Cmd{kind: cmdKeyUp, a: int(keysym)}) }
func (s *InputSender) MouseDown(button int)  { s.send(x11Cmd{kind: cmdBtnDown, a: button}) }
func (s *InputSender) MouseUp(button int)    { s.send(x11Cmd{kind: cmdBtnUp, a: button}) }
func (s *InputSender) MouseMove(dx, dy int)  { s.send(x11Cmd{kind: cmdMouseRel, a: dx, b: dy}) }
func (s *InputSender) WarpAbsolute(x, y int) { s.send(x11Cmd{kind: cmdMouseAbs, a: x, b: y}) }

// ClickAt moves cursor to (x,y) and performs a full click with 80ms hold.
// This is atomic in the X11 worker — guarantees cursor is at target before click.
func (s *InputSender) ClickAt(x, y, button int) {
	s.send(x11Cmd{kind: cmdClickAt, a: x, b: y, c: button})
}

func (s *InputSender) FocusGame()            { s.send(x11Cmd{kind: cmdFocus}) }
func (s *InputSender) ListWindows()          { s.send(x11Cmd{kind: cmdListWindows}) }
func (s *InputSender) InvalidateWindowCache() { s.send(x11Cmd{kind: cmdInvalidateCache}) }

func (s *InputSender) HasCS2Window() bool {
	for len(s.hasCS2Chan) > 0 {
		<-s.hasCS2Chan
	}
	s.send(x11Cmd{kind: cmdHasCS2})
	select {
	case result := <-s.hasCS2Chan:
		return result
	case <-time.After(5 * time.Second):
		return false
	}
}

// TypeLine types a string into the focused window and presses Enter.
func (s *InputSender) TypeLine(text string) { s.send(x11Cmd{kind: cmdTypeLine, str: text}) }

// GrabCS2CenterGray captures a roiW×roiH grayscale patch from the center of the CS2 window.
// Returns nil on failure or if X is busy. Cheap enough for ~8–12 Hz per sandbox.
func (s *InputSender) GrabCS2CenterGray(roiW, roiH int) []byte {
	for len(s.grabGrayChan) > 0 {
		<-s.grabGrayChan
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || roiW < 8 || roiH < 8 {
		return nil
	}
	select {
	case s.cmds <- x11Cmd{kind: cmdGrabGray, a: roiW, b: roiH}:
	case <-time.After(2 * time.Second):
		return nil
	}
	select {
	case data := <-s.grabGrayChan:
		if len(data) == 0 {
			return nil
		}
		out := make([]byte, len(data))
		copy(out, data)
		return out
	case <-time.After(2 * time.Second):
		return nil
	}
}

// GrabCS2GrayRect grabs roiW×roiH grayscale at top-left (x0,y0) in the CS2 window (client coords).
func (s *InputSender) GrabCS2GrayRect(x0, y0, roiW, roiH int) []byte {
	for len(s.grabGrayChan) > 0 {
		<-s.grabGrayChan
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || roiW < 8 || roiH < 8 || x0 < 0 || y0 < 0 {
		return nil
	}
	select {
	case s.cmds <- x11Cmd{kind: cmdGrabGrayRect, a: x0, b: y0, c: roiW, d: roiH}:
	case <-time.After(2 * time.Second):
		return nil
	}
	select {
	case data := <-s.grabGrayChan:
		if len(data) == 0 {
			return nil
		}
		out := make([]byte, len(data))
		copy(out, data)
		return out
	case <-time.After(2 * time.Second):
		return nil
	}
}

// GrabCS2RGBRect grabs interleaved RGB (len roiW*roiH*3) at (x0,y0) in the CS2 window.
func (s *InputSender) GrabCS2RGBRect(x0, y0, roiW, roiH int) []byte {
	for len(s.grabRGBChan) > 0 {
		<-s.grabRGBChan
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || roiW < 8 || roiH < 8 || x0 < 0 || y0 < 0 {
		return nil
	}
	select {
	case s.cmds <- x11Cmd{kind: cmdGrabRGBRect, a: x0, b: y0, c: roiW, d: roiH}:
	case <-time.After(2 * time.Second):
		return nil
	}
	select {
	case data := <-s.grabRGBChan:
		if len(data) == 0 {
			return nil
		}
		out := make([]byte, len(data))
		copy(out, data)
		return out
	case <-time.After(2 * time.Second):
		return nil
	}
}

// CS2ClientPixelSize returns the CS2 window client width/height in pixels (same coords as GrabCS2* / overlay).
func (s *InputSender) CS2ClientPixelSize() (w, h int, ok bool) {
	for len(s.cs2SizeChan) > 0 {
		<-s.cs2SizeChan
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, 0, false
	}
	select {
	case s.cmds <- x11Cmd{kind: cmdCS2ClientSize}:
	case <-time.After(2 * time.Second):
		return 0, 0, false
	}
	select {
	case p := <-s.cs2SizeChan:
		if p[0] < 320 || p[1] < 240 {
			return 0, 0, false
		}
		return p[0], p[1], true
	case <-time.After(2 * time.Second):
		return 0, 0, false
	}
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
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.send(x11Cmd{kind: cmdQuit})

	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		log.Printf("[X11Input] Close timed out (worker stuck in error handler)")
	}
}
