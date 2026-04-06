import { useEffect, useRef, useCallback, useState } from 'react';
import RFB from '@novnc/novnc/lib/rfb';

interface VncViewerProps {
  port: number;
  className?: string;
}

export default function VncViewer({ port, className }: VncViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting');

  const connect = useCallback(() => {
    if (!containerRef.current) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/vnc/${port}`;

    try {
      const rfb = new RFB(containerRef.current, url, {
        wsProtocols: [],
      });

      rfb.viewOnly = true;
      rfb.scaleViewport = true;
      rfb.resizeSession = false;
      rfb.showDotCursor = false;

      rfb.addEventListener('connect', () => setStatus('connected'));
      rfb.addEventListener('disconnect', () => {
        setStatus('disconnected');
        rfbRef.current = null;
      });
      rfb.addEventListener('securityfailure', () => setStatus('error'));

      rfbRef.current = rfb;
      setStatus('connecting');
    } catch {
      setStatus('error');
    }
  }, [port]);

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
    <div className={className}>
      <div ref={containerRef} className="w-full h-full bg-black rounded-lg overflow-hidden" />
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
