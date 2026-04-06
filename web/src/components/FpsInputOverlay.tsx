import { useEffect, useRef, useCallback, useState } from 'react';

interface FpsInputOverlayProps {
  display: number;
  active: boolean;
  onLockChange?: (locked: boolean) => void;
}

/**
 * Transparent overlay that captures mouse via Pointer Lock and sends
 * relative mouse deltas + button events to the xinput_relay backend
 * via a dedicated WebSocket.
 */
export default function FpsInputOverlay({ display, active, onLockChange }: FpsInputOverlayProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [locked, setLocked] = useState(false);
  const sendBuf = useRef<string[]>([]);
  const flushTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  const wsReady = useCallback(() => wsRef.current?.readyState === WebSocket.OPEN, []);

  const send = useCallback((msg: object) => {
    if (wsReady()) {
      wsRef.current!.send(JSON.stringify(msg));
    } else {
      sendBuf.current.push(JSON.stringify(msg));
    }
  }, [wsReady]);

  useEffect(() => {
    if (!active) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/ws/input/${display}`;
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      for (const m of sendBuf.current) ws.send(m);
      sendBuf.current = [];
    };
    ws.onclose = () => { wsRef.current = null; };

    return () => {
      ws.close();
      wsRef.current = null;
    };
  }, [active, display]);

  useEffect(() => {
    const el = overlayRef.current;
    if (!el) return;

    const onLockChangeEvt = () => {
      const isLocked = document.pointerLockElement === el;
      setLocked(isLocked);
      onLockChange?.(isLocked);
    };

    const onMouseMove = (e: MouseEvent) => {
      if (document.pointerLockElement !== el) return;
      const dx = e.movementX;
      const dy = e.movementY;
      if (dx !== 0 || dy !== 0) {
        send({ t: 'm', dx, dy });
      }
    };

    const onMouseDown = (e: MouseEvent) => {
      if (document.pointerLockElement !== el) return;
      e.preventDefault();
      const btn = mouseButtonMap(e.button);
      if (btn) send({ t: 'bd', b: btn });
    };

    const onMouseUp = (e: MouseEvent) => {
      if (document.pointerLockElement !== el) return;
      e.preventDefault();
      const btn = mouseButtonMap(e.button);
      if (btn) send({ t: 'bu', b: btn });
    };

    document.addEventListener('pointerlockchange', onLockChangeEvt);
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mousedown', onMouseDown);
    document.addEventListener('mouseup', onMouseUp);

    return () => {
      document.removeEventListener('pointerlockchange', onLockChangeEvt);
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mousedown', onMouseDown);
      document.removeEventListener('mouseup', onMouseUp);
      if (flushTimer.current) clearInterval(flushTimer.current);
    };
  }, [send, onLockChange]);

  const requestLock = useCallback(() => {
    overlayRef.current?.requestPointerLock();
  }, []);

  const exitLock = useCallback(() => {
    if (document.pointerLockElement) {
      document.exitPointerLock();
    }
  }, []);

  if (!active) return null;

  return (
    <div
      ref={overlayRef}
      onClick={locked ? undefined : requestLock}
      onContextMenu={(e) => e.preventDefault()}
      className="absolute inset-0 z-10"
      style={{ cursor: locked ? 'none' : 'crosshair' }}
    >
      {locked && (
        <div className="absolute top-3 left-1/2 -translate-x-1/2 px-3 py-1 rounded bg-black/60 text-xs text-green-400 pointer-events-none select-none">
          FPS mode — press ESC to release
        </div>
      )}
      {!locked && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <div className="px-4 py-2 rounded-lg bg-black/70 text-sm text-text-primary border border-accent/30 select-none">
            Click to capture mouse (FPS mode)
          </div>
        </div>
      )}
    </div>
  );
}

function mouseButtonMap(browserBtn: number): number | null {
  switch (browserBtn) {
    case 0: return 1; // left
    case 1: return 2; // middle
    case 2: return 3; // right
    default: return null;
  }
}
