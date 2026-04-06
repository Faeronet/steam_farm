#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * Persistent X11 input relay. Reads commands from stdin line-by-line:
 *   M dx dy          - relative mouse move
 *   B button down    - mouse button press (1=left,2=mid,3=right)
 *   b button up      - mouse button release
 *   K keysym down    - key press
 *   k keysym up      - key release
 *   Q               - quit
 */
int main(int argc, char *argv[]) {
    const char *dispname = argc > 1 ? argv[1] : NULL;
    Display *d = XOpenDisplay(dispname);
    if (!d) {
        fprintf(stderr, "xinput_relay: cannot open display %s\n", dispname ? dispname : "(default)");
        return 1;
    }

    setbuf(stdin, NULL);
    setbuf(stdout, NULL);

    printf("READY\n");

    char line[256];
    while (fgets(line, sizeof(line), stdin)) {
        char cmd = line[0];
        if (cmd == 'M') {
            int dx = 0, dy = 0;
            sscanf(line + 1, "%d %d", &dx, &dy);
            XWarpPointer(d, None, None, 0, 0, 0, 0, dx, dy);
            XFlush(d);
        } else if (cmd == 'B') {
            int btn = 0;
            sscanf(line + 1, "%d", &btn);
            XTestFakeButtonEvent(d, btn, True, CurrentTime);
            XFlush(d);
        } else if (cmd == 'b') {
            int btn = 0;
            sscanf(line + 1, "%d", &btn);
            XTestFakeButtonEvent(d, btn, False, CurrentTime);
            XFlush(d);
        } else if (cmd == 'K') {
            unsigned long ks = 0;
            sscanf(line + 1, "%lu", &ks);
            XTestFakeKeyEvent(d, XKeysymToKeycode(d, ks), True, CurrentTime);
            XFlush(d);
        } else if (cmd == 'k') {
            unsigned long ks = 0;
            sscanf(line + 1, "%lu", &ks);
            XTestFakeKeyEvent(d, XKeysymToKeycode(d, ks), False, CurrentTime);
            XFlush(d);
        } else if (cmd == 'Q') {
            break;
        }
    }

    XCloseDisplay(d);
    return 0;
}
