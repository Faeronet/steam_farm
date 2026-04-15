//go:build linux

package autoplay

/*
#cgo LDFLAGS: -lX11 -lXext
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/extensions/shape.h>
#include <stdlib.h>
#include <string.h>

// Макросы Xlib не видны cgo — только функции.
static int ov_DefaultScreen(Display *d) { return DefaultScreen(d); }
static Window ov_DefaultRootWindow(Display *d) { return DefaultRootWindow(d); }
static int ov_DisplayWidth(Display *d, int snum) { return DisplayWidth(d, snum); }
static int ov_DisplayHeight(Display *d, int snum) { return DisplayHeight(d, snum); }
static unsigned long ov_BlackPixel(Display *d, int snum) { return BlackPixel(d, snum); }
static unsigned long ov_WhitePixel(Display *d, int snum) { return WhitePixel(d, snum); }
static Colormap ov_DefaultColormap(Display *d, int snum) { return DefaultColormap(d, snum); }

static Window ov_create_overlay_window(Display *d, Window parent, unsigned w, unsigned h,
				       unsigned long bg, unsigned long border) {
	XSetWindowAttributes wa;
	memset(&wa, 0, sizeof(wa));
	wa.override_redirect = True;
	wa.background_pixel = bg;
	wa.border_pixel = border;
	return XCreateWindow(d, parent, 0, 0, w, h, 0, CopyFromParent, InputOutput,
			     CopyFromParent, CWOverrideRedirect | CWBackPixel | CWBorderPixel, &wa);
}

static int ov_err(Display *d, XErrorEvent *e) {
	(void)d;
	(void)e;
	return 0;
}

static void ov_install_error_handler(void) { XSetErrorHandler(ov_err); }

static int ov_alloc_red_pixel(Display *d, int screen, unsigned long *pixel_out) {
	Colormap cm = ov_DefaultColormap(d, screen);
	XColor near_xc, exact_xc;
	char nm[] = "red";
	if (XAllocNamedColor(d, cm, nm, &near_xc, &exact_xc)) {
		*pixel_out = near_xc.pixel;
		return 0;
	}
	*pixel_out = ov_WhitePixel(d, screen);
	return -1;
}

static int ov_name_matches_cs2(const char *name) {
	if (!name) return 0;
	if (strstr(name, "Counter-Strike")) return 1;
	if (strstr(name, "counter-strike")) return 1;
	if (strstr(name, "CS2") || strstr(name, "cs2")) return 1;
	return 0;
}

static int ov_window_matches_cs2(Display *d, Window w) {
	XWindowAttributes attr;
	if (!XGetWindowAttributes(d, w, &attr) || attr.map_state != IsViewable)
		return 0;

	char *wm_name = NULL;
	if (XFetchName(d, w, &wm_name) && wm_name) {
		int m = ov_name_matches_cs2(wm_name);
		XFree(wm_name);
		if (m) return 1;
	}

	XClassHint class_hint;
	memset(&class_hint, 0, sizeof(class_hint));
	if (XGetClassHint(d, w, &class_hint)) {
		int m = ov_name_matches_cs2(class_hint.res_name) ||
		        ov_name_matches_cs2(class_hint.res_class);
		if (class_hint.res_name) XFree(class_hint.res_name);
		if (class_hint.res_class) XFree(class_hint.res_class);
		if (m) return 1;
	}

	Atom utf8 = XInternAtom(d, "UTF8_STRING", False);
	Atom net_wm_name = XInternAtom(d, "_NET_WM_NAME", False);
	Atom act_type;
	int act_fmt;
	unsigned long nitems, bytes_after;
	unsigned char *prop = NULL;
	int gp = XGetWindowProperty(d, w, net_wm_name, 0, 1024, False, utf8, &act_type,
	                          &act_fmt, &nitems, &bytes_after, &prop);
	if (gp == Success && prop) {
		if (act_fmt == 8 && nitems > 0 && ov_name_matches_cs2((char *)prop)) {
			XFree(prop);
			return 1;
		}
		XFree(prop);
	}
	return 0;
}

static Window ov_find_cs2(Display *d, Window w, int depth, int max_depth) {
	if (depth > max_depth) return 0;
	if (ov_window_matches_cs2(d, w)) return w;

	Window root_ret, parent_ret;
	Window *children = NULL;
	unsigned int nchildren = 0;
	if (!XQueryTree(d, w, &root_ret, &parent_ret, &children, &nchildren))
		return 0;

	Window found = 0;
	for (unsigned int i = 0; i < nchildren && !found; i++) {
		found = ov_find_cs2(d, children[i], depth + 1, max_depth);
	}
	if (children) XFree(children);
	return found;
}

static Window ov_find_cs2_root(Display *d) {
	return ov_find_cs2(d, ov_DefaultRootWindow(d), 0, 24);
}

static int ov_root_xy(Display *d, Window cs2, int wx, int wy, int *rx, int *ry) {
	Window child = 0;
	if (!XTranslateCoordinates(d, cs2, ov_DefaultRootWindow(d), wx, wy, rx, ry, &child))
		return -1;
	return 0;
}

// Union hollow rectangles (frame) for each root-axis-aligned box into out.
static void ov_union_boxes_into_region(Region out, int n, const int *xyxy, int thick) {
	for (int i = 0; i < n; i++) {
		int x1 = xyxy[i * 4 + 0], y1 = xyxy[i * 4 + 1];
		int x2 = xyxy[i * 4 + 2], y2 = xyxy[i * 4 + 3];
		if (x2 <= x1 || y2 <= y1)
			continue;
		if (thick < 1) thick = 2;
		XRectangle rects[4] = {
		    {(short)x1, (short)y1, (unsigned short)(x2 - x1), (unsigned short)thick},
		    {(short)x1, (short)(y2 - thick), (unsigned short)(x2 - x1), (unsigned short)thick},
		    {(short)x1, (short)y1, (unsigned short)thick, (unsigned short)(y2 - y1)},
		    {(short)(x2 - thick), (short)y1, (unsigned short)thick, (unsigned short)(y2 - y1)},
		};
		for (int k = 0; k < 4; k++) {
			if ((int)rects[k].width <= 0 || (int)rects[k].height <= 0)
				continue;
			Region b = XCreateRegion();
			XUnionRectWithRegion(&rects[k], b, b);
			XUnionRegion(out, b, out);
			XDestroyRegion(b);
		}
	}
}

static void ov_stack_above(Display *d, Window overlay, Window sibling) {
	if (!sibling) return;
	XWindowChanges wc;
	wc.sibling = sibling;
	wc.stack_mode = Above;
	XConfigureWindow(d, overlay, CWSibling | CWStackMode, &wc);
}
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const overlayMaxBoxes = 24

var overlayDebugLastLog time.Time

func overlayDebugOn() bool {
	v := strings.TrimSpace(os.Getenv("SFARM_CS2_OVERLAY_DEBUG"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

type linuxEnemyOverlay struct {
	display int
	ch      chan overlayFrame
	once    sync.Once
	done    chan struct{}
}

type overlayFrame struct {
	roiX, roiY, rw, rh int
	dets               []YoloDet
}

func overlayEnvOn() bool {
	v := strings.TrimSpace(os.Getenv("SFARM_CS2_OVERLAY"))
	if v == "" {
		return true
	}
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") || strings.EqualFold(v, "off") {
		return false
	}
	return true
}

// NewEnemyOverlay — оконный слой поверх CS2 на DISPLAY (видно в VNC). По умолчанию включён; выкл: SFARM_CS2_OVERLAY=0|off|false.
func NewEnemyOverlay(display int) EnemyOverlay {
	if !overlayEnvOn() {
		return nil
	}
	o := &linuxEnemyOverlay{
		display: display,
		ch:      make(chan overlayFrame, 4),
		done:    make(chan struct{}),
	}
	go o.run()
	return o
}

func (o *linuxEnemyOverlay) Close() {
	o.once.Do(func() { close(o.done) })
}

func (o *linuxEnemyOverlay) PushYolo(roiX, roiY, rw, rh int, viz []YoloDet) {
	n := len(viz)
	if n > overlayMaxBoxes {
		n = overlayMaxBoxes
	}
	cp := make([]YoloDet, n)
	copy(cp, viz[:n])
	fr := overlayFrame{roiX: roiX, roiY: roiY, rw: rw, rh: rh, dets: cp}
	select {
	case o.ch <- fr:
	default:
		select {
		case <-o.ch:
		default:
		}
		select {
		case o.ch <- fr:
		default:
		}
	}
}

func (o *linuxEnemyOverlay) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	x11PrepareClientEnv()

	C.ov_install_error_handler()
	var d *C.Display
	for _, cand := range []string{
		fmt.Sprintf(":%d", o.display),
		fmt.Sprintf("127.0.0.1:%d.0", o.display),
	} {
		name := C.CString(cand)
		d = C.XOpenDisplay(name)
		C.free(unsafe.Pointer(name))
		if d != nil {
			log.Printf("[CS2Overlay] Opened display %q", cand)
			break
		}
	}
	if d == nil {
		log.Printf("[CS2Overlay] XOpenDisplay :%d failed (unix+tcp)", o.display)
		return
	}
	defer C.XCloseDisplay(d)

	var evBase, errBase C.int
	if C.XShapeQueryExtension(d, &evBase, &errBase) == 0 {
		log.Printf("[CS2Overlay] XShape extension missing; overlay disabled")
		return
	}

	screen := C.ov_DefaultScreen(d)
	root := C.ov_DefaultRootWindow(d)
	sw := C.ov_DisplayWidth(d, screen)
	sh := C.ov_DisplayHeight(d, screen)

	borderPx := C.ov_BlackPixel(d, screen)
	var redPx C.ulong
	C.ov_alloc_red_pixel(d, screen, &redPx)
	ov := C.ov_create_overlay_window(d, root, C.uint(sw), C.uint(sh), redPx, borderPx)

	empty := C.XCreateRegion()
	C.XShapeCombineRegion(d, ov, C.ShapeInput, 0, 0, empty, C.ShapeSet)
	C.XDestroyRegion(empty)

	C.XMapWindow(d, ov)
	C.XRaiseWindow(d, ov)
	C.XFlush(d)

	boxBuf := make([]C.int, overlayMaxBoxes*4)

	for {
		select {
		case <-o.done:
			C.XDestroyWindow(d, ov)
			C.XFlush(d)
			return
		case fr := <-o.ch:
			cs2 := C.ov_find_cs2_root(d)
			n := 0
			if cs2 != 0 {
				corners := [4][2]int{}
				for _, det := range fr.dets {
					if len(det.Xyxy) < 4 || n >= overlayMaxBoxes {
						continue
					}
					x1 := int(det.Xyxy[0])
					y1 := int(det.Xyxy[1])
					x2 := int(det.Xyxy[2])
					y2 := int(det.Xyxy[3])
					if fr.rw > 0 && fr.rh > 0 {
						if x1 < 0 {
							x1 = 0
						}
						if y1 < 0 {
							y1 = 0
						}
						if x2 > fr.rw {
							x2 = fr.rw
						}
						if y2 > fr.rh {
							y2 = fr.rh
						}
					}
					if x2 <= x1 || y2 <= y1 {
						continue
					}
					corners[0] = [2]int{fr.roiX + x1, fr.roiY + y1}
					corners[1] = [2]int{fr.roiX + x2, fr.roiY + y1}
					corners[2] = [2]int{fr.roiX + x2, fr.roiY + y2}
					corners[3] = [2]int{fr.roiX + x1, fr.roiY + y2}
					var rxMin, ryMin, rxMax, ryMax int
					ok := true
					for ci := 0; ci < 4; ci++ {
						var rx, ry C.int
						if C.ov_root_xy(d, cs2, C.int(corners[ci][0]), C.int(corners[ci][1]), &rx, &ry) != 0 {
							ok = false
							break
						}
						ix, iy := int(rx), int(ry)
						if ci == 0 {
							rxMin, ryMin, rxMax, ryMax = ix, iy, ix, iy
						} else {
							if ix < rxMin {
								rxMin = ix
							}
							if iy < ryMin {
								ryMin = iy
							}
							if ix > rxMax {
								rxMax = ix
							}
							if iy > ryMax {
								ryMax = iy
							}
						}
					}
					if !ok || rxMax <= rxMin || ryMax <= ryMin {
						continue
					}
					boxBuf[n*4+0] = C.int(rxMin)
					boxBuf[n*4+1] = C.int(ryMin)
					boxBuf[n*4+2] = C.int(rxMax)
					boxBuf[n*4+3] = C.int(ryMax)
					n++
				}
			}

			reg := C.XCreateRegion()
			if n > 0 {
				C.ov_union_boxes_into_region(reg, C.int(n), (*C.int)(unsafe.Pointer(&boxBuf[0])), 3)
			}
			C.XShapeCombineRegion(d, ov, C.ShapeBounding, 0, 0, reg, C.ShapeSet)
			C.XDestroyRegion(reg)
			C.XClearWindow(d, ov)
			if cs2 != 0 {
				C.ov_stack_above(d, ov, cs2)
			} else {
				C.XRaiseWindow(d, ov)
			}
			C.XFlush(d)
			if overlayDebugOn() {
				tnow := time.Now()
				if overlayDebugLastLog.IsZero() || tnow.Sub(overlayDebugLastLog) >= 2*time.Second {
					overlayDebugLastLog = tnow
					log.Printf("[CS2Overlay:%d] shape boxes=%d cs2_win=0x%x viz_dets=%d roi=%d,%d %dx%d",
						o.display, n, uint64(cs2), len(fr.dets), fr.roiX, fr.roiY, fr.rw, fr.rh)
				}
			}
		}
	}
}
