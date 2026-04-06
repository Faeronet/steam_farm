import { useState, useEffect, useCallback, useRef } from 'react';
import { useWebSocket } from './useWebSocket';

export interface LogEntry {
  time: string;
  level: string;
  message: string;
}

export function useFarmLogs(maxEntries = 200) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const { connected, subscribe } = useWebSocket();
  const initialLoaded = useRef(false);

  useEffect(() => {
    if (initialLoaded.current) return;
    initialLoaded.current = true;
    fetch('/api/logs?n=50')
      .then(r => r.json())
      .then((entries: LogEntry[]) => {
        if (Array.isArray(entries) && entries.length > 0) {
          setLogs(entries);
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === 'log') {
        const entry = msg.payload as LogEntry;
        setLogs(prev => [...prev.slice(-(maxEntries - 1)), entry]);
      }
    });
  }, [subscribe, maxEntries]);

  const addLocal = useCallback((level: string, message: string) => {
    setLogs(prev => [...prev.slice(-(maxEntries - 1)), {
      time: new Date().toLocaleTimeString(),
      level,
      message,
    }]);
  }, [maxEntries]);

  const clear = useCallback(() => setLogs([]), []);

  return { logs, connected, addLocal, clear };
}
