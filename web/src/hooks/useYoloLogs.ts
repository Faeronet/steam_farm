import { useState, useEffect, useCallback, useRef } from 'react';
import { useWebSocket } from './useWebSocket';
import type { LogEntry } from './useFarmLogs';

export function useYoloLogs(maxEntries = 200) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const { connected, subscribe } = useWebSocket();
  const initialLoaded = useRef(false);

  useEffect(() => {
    if (initialLoaded.current) return;
    initialLoaded.current = true;
    fetch('/api/yolo-logs?n=80')
      .then((r) => r.json())
      .then((entries: LogEntry[]) => {
        if (Array.isArray(entries) && entries.length > 0) {
          setLogs(entries);
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === 'yolo:log') {
        const entry = msg.payload as LogEntry;
        setLogs((prev) => [...prev.slice(-(maxEntries - 1)), entry]);
      }
    });
  }, [subscribe, maxEntries]);

  const clear = useCallback(() => setLogs([]), []);

  return { logs, connected, clear };
}
