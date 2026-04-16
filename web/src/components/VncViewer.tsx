import { useEffect, useRef, useCallback, useState } from 'react';
import RFB from '@novnc/novnc/lib/rfb';

interface VncViewerProps {
  port: number;
  className?: string;
  viewOnly?: boolean;
  onStatusChange?: (status: string) => void;
}

export default function VncViewer({ port, className, viewOnly = false, onStatusChange }: VncViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting');

  const updateStatus = useCallback((s: 'connecting' | 'connected' | 'disconnected' | 'error') => {
    setStatus(s);
    onStatusChange?.(s);
  }, [onStatusChange]);

  const connect = useCallback(() => {
    if (!containerRef.current) return;
    if (rfbRef.current) {
      rfbRef.current.disconnect();
      rfbRef.current = null;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/vnc/${port}`;

    try {
      // Без wsProtocols: пустой массив в WebSocket(url, []) в части браузеров ломает рукопожатие.
      const rfb = new RFB(containerRef.current, url);

      rfb.viewOnly = viewOnly;
      rfb.scaleViewport = true;
      rfb.resizeSession = false;
      rfb.showDotCursor = !viewOnly;
      rfb.focusOnClick = true;
      rfb.qualityLevel = 6;
      rfb.compressionLevel = 2;

      rfb.addEventListener('connect', () => updateStatus('connected'));
      rfb.addEventListener('disconnect', () => {
        updateStatus('disconnected');
        rfbRef.current = null;
      });
      rfb.addEventListener('securityfailure', () => updateStatus('error'));

      rfbRef.current = rfb;
      updateStatus('connecting');
    } catch {
      updateStatus('error');
    }
  }, [port, viewOnly, updateStatus]);

  useEffect(() => {
    connect();
    return () => {
      if (rfbRef.current) {
        rfbRef.current.disconnect();
        rfbRef.current = null;
      }
    };
  }, [connect]);

  return (
    <div className={`relative min-h-0 ${className ?? ''}`}>
      <div
        ref={containerRef}
        className="w-full h-full bg-black rounded-lg overflow-hidden min-h-[120px]"
        style={{ cursor: viewOnly ? 'default' : 'crosshair' }}
      />
      {status !== 'connected' && (
        <div className="absolute inset-0 flex items-center justify-center bg-bg-primary/80 rounded-lg">
          {status === 'connecting' && (
            <p className="text-sm text-text-muted animate-pulse">Connecting to VNC :{port}...</p>
          )}
          {status === 'disconnected' && (
            <div className="text-center">
              <p className="text-sm text-text-muted mb-2">VNC disconnected</p>
              <button onClick={connect} className="btn-primary text-xs">Reconnect</button>
            </div>
          )}
          {status === 'error' && (
            <div className="text-center">
              <p className="text-sm text-red-400 mb-2">VNC connection failed</p>
              <button onClick={connect} className="btn-primary text-xs">Retry</button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
