import { useEffect, useRef, useCallback, useSyncExternalStore } from 'react';

interface WSMessage {
  type: string;
  payload: unknown;
}

type MessageHandler = (msg: WSMessage) => void;

let globalWs: WebSocket | null = null;
let globalConnected = false;
const handlers = new Set<MessageHandler>();
const connListeners = new Set<() => void>();
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let destroyed = false;
let refCount = 0;

function notifyConnListeners() {
  connListeners.forEach((l) => l());
}

function connectGlobal(path: string) {
  if (destroyed || globalWs?.readyState === WebSocket.OPEN || globalWs?.readyState === WebSocket.CONNECTING) {
    return;
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${protocol}//${window.location.host}${path}`;
  const ws = new WebSocket(url);

  ws.onopen = () => {
    if (globalWs !== ws) return;
    globalConnected = true;
    notifyConnListeners();
  };

  ws.onclose = () => {
    if (globalWs !== ws) return;
    globalConnected = false;
    globalWs = null;
    notifyConnListeners();

    if (!destroyed && refCount > 0) {
      if (reconnectTimer) clearTimeout(reconnectTimer);
      reconnectTimer = setTimeout(() => connectGlobal(path), 3000);
    }
  };

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data) as WSMessage;
      handlers.forEach((h) => h(msg));
    } catch {
      // ignore
    }
  };

  globalWs = ws;
}

function destroyGlobal() {
  destroyed = true;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  const ws = globalWs;
  globalWs = null;
  globalConnected = false;
  ws?.close();
}

function getConnected() {
  return globalConnected;
}

function subscribeConn(cb: () => void) {
  connListeners.add(cb);
  return () => { connListeners.delete(cb); };
}

export function useWebSocket(path = '/ws') {
  const pathRef = useRef(path);

  useEffect(() => {
    destroyed = false;
    refCount++;
    connectGlobal(pathRef.current);
    return () => {
      refCount--;
      if (refCount <= 0) {
        refCount = 0;
        destroyGlobal();
      }
    };
  }, []);

  const connected = useSyncExternalStore(subscribeConn, getConnected);

  const subscribe = useCallback((handler: MessageHandler) => {
    handlers.add(handler);
    return () => { handlers.delete(handler); };
  }, []);

  return { connected, subscribe };
}
