import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, useEffect, useCallback } from 'react';
import { cn } from '@/lib/utils';
import { Monitor, Play, Square, RefreshCw, Cpu, HardDrive, Maximize2, Minimize2, X, Crosshair, Bot, Heart, Clock, Gamepad2 } from 'lucide-react';
import VncViewer from '@/components/VncViewer';
import FpsInputOverlay from '@/components/FpsInputOverlay';

interface ContainerInfo {
  id: string;
  name: string;
  account_id: number;
  game_type: string;
  status: string;
  vnc_port: number;
  display: string;
  cpu_percent: number;
  memory_mb: number;
  hostname: string;
}

async function fetchSandboxes(): Promise<ContainerInfo[]> {
  const res = await fetch('/api/sandbox/list');
  return res.json();
}

async function stopSandbox(accountId: number) {
  const res = await fetch('/api/sandbox/stop', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_id: accountId }),
  });
  return res.json();
}

interface BotStatus {
  display: number;
  steam_id: string;
  phase: string;
  running: boolean;
  uptime: string;
  kills: number;
  deaths: number;
  health: number;
  map: string;
}

async function fetchAutoplayStatus(): Promise<BotStatus[]> {
  const res = await fetch('/api/autoplay/status');
  return res.json();
}

async function autoplayStart(accountId: number) {
  return fetch('/api/autoplay/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_id: accountId }),
  }).then(r => r.json());
}

async function autoplayStop(accountId: number) {
  return fetch('/api/autoplay/stop', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_id: accountId }),
  }).then(r => r.json());
}

function parseDisplayNum(display: string): number {
  const n = parseInt(display.replace(':', ''), 10);
  return isNaN(n) ? 100 : n;
}

function VncFullscreenModal({ port, display, onClose }: { port: number; display: string; onClose: () => void }) {
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [fpsMode, setFpsMode] = useState(false);
  const [pointerLocked, setPointerLocked] = useState(false);
  const displayNum = parseDisplayNum(display);

  const toggleFullscreen = useCallback(() => {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen().then(() => setIsFullscreen(true)).catch(() => {});
    } else {
      document.exitFullscreen().then(() => setIsFullscreen(false)).catch(() => {});
    }
  }, []);

  useEffect(() => {
    const onFsChange = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', onFsChange);

    const onEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !document.fullscreenElement && !pointerLocked) {
        onClose();
      }
    };
    document.addEventListener('keydown', onEsc);

    return () => {
      document.removeEventListener('fullscreenchange', onFsChange);
      document.removeEventListener('keydown', onEsc);
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      }
    };
  }, [onClose, pointerLocked]);

  return (
    <div className="fixed inset-0 z-50 bg-black flex flex-col">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 bg-bg-secondary border-b border-border-default shrink-0">
        <div className="flex items-center gap-3">
          <Monitor className="w-4 h-4 text-accent" />
          <span className="text-sm font-semibold text-text-primary">
            VNC — Port {port}
          </span>
          <span className="text-xs text-text-muted">
            {fpsMode ? 'FPS mode — click inside to capture' : 'Click inside to interact with Steam'}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setFpsMode(!fpsMode)}
            className={cn(
              'flex items-center gap-1.5 px-2.5 py-1.5 rounded text-xs font-medium transition-colors',
              fpsMode
                ? 'bg-accent/20 text-accent border border-accent/40'
                : 'hover:bg-bg-tertiary text-text-muted hover:text-text-primary'
            )}
            title="Toggle FPS mouse mode (relative mouse for camera control)"
          >
            <Crosshair className="w-3.5 h-3.5" />
            FPS
          </button>
          <button
            onClick={toggleFullscreen}
            className="p-1.5 rounded hover:bg-bg-tertiary text-text-muted hover:text-text-primary transition-colors"
            title={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
          </button>
          <button
            onClick={onClose}
            className="p-1.5 rounded hover:bg-red-500/20 text-text-muted hover:text-red-400 transition-colors"
            title="Close (Esc)"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* VNC Canvas + FPS overlay */}
      <div className="flex-1 relative overflow-hidden">
        <VncViewer
          key={`fullscreen-${port}`}
          port={port}
          viewOnly={fpsMode}
          className="w-full h-full relative"
        />
        <FpsInputOverlay
          display={displayNum}
          active={fpsMode}
          onLockChange={setPointerLocked}
        />
      </div>
    </div>
  );
}

export default function SandboxMonitor() {
  const queryClient = useQueryClient();
  const [selectedVNC, setSelectedVNC] = useState<number | null>(null);
  const [fullscreenVNC, setFullscreenVNC] = useState<{ port: number; display: string } | null>(null);

  const { data: containers, isLoading } = useQuery({
    queryKey: ['sandboxes'],
    queryFn: fetchSandboxes,
    refetchInterval: 3000,
  });

  const stopMutation = useMutation({
    mutationFn: (accountId: number) => stopSandbox(accountId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sandboxes'] }),
  });

  const { data: botStatuses } = useQuery({
    queryKey: ['autoplay-status'],
    queryFn: fetchAutoplayStatus,
    refetchInterval: 2000,
  });

  const apStartMutation = useMutation({
    mutationFn: (accountId: number) => autoplayStart(accountId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['autoplay-status'] }),
  });

  const apStopMutation = useMutation({
    mutationFn: (accountId: number) => autoplayStop(accountId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['autoplay-status'] }),
  });

  const totalCPU = containers?.reduce((s, c) => s + c.cpu_percent, 0) ?? 0;
  const totalRAM = containers?.reduce((s, c) => s + c.memory_mb, 0) ?? 0;

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Fullscreen VNC Modal */}
      {fullscreenVNC && (
        <VncFullscreenModal
          port={fullscreenVNC.port}
          display={fullscreenVNC.display}
          onClose={() => setFullscreenVNC(null)}
        />
      )}

      {/* Summary bar */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
        <div className="card flex items-center gap-3">
          <div className="w-10 h-10 rounded-lg bg-accent/10 flex items-center justify-center">
            <Monitor className="w-5 h-5 text-accent" />
          </div>
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider">Containers</p>
            <p className="text-xl font-bold">{containers?.length ?? 0}</p>
          </div>
        </div>
        <div className="card flex items-center gap-3">
          <div className="w-10 h-10 rounded-lg bg-status-active/10 flex items-center justify-center">
            <Play className="w-5 h-5 text-status-active" />
          </div>
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider">Running</p>
            <p className="text-xl font-bold">{containers?.filter(c => c.status === 'running').length ?? 0}</p>
          </div>
        </div>
        <div className="card flex items-center gap-3">
          <div className="w-10 h-10 rounded-lg bg-game-cs2/10 flex items-center justify-center">
            <Cpu className="w-5 h-5 text-game-cs2" />
          </div>
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider">Total CPU</p>
            <p className="text-xl font-bold">{totalCPU.toFixed(1)}%</p>
          </div>
        </div>
        <div className="card flex items-center gap-3">
          <div className="w-10 h-10 rounded-lg bg-status-farmed/10 flex items-center justify-center">
            <HardDrive className="w-5 h-5 text-status-farmed" />
          </div>
          <div>
            <p className="text-xs text-text-muted uppercase tracking-wider">Total RAM</p>
            <p className="text-xl font-bold">{(totalRAM / 1024).toFixed(1)} GB</p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Container table */}
        <div className="lg:col-span-2 card !p-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-border-default flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2">
              <Monitor className="w-4 h-4 text-accent" /> Active Containers
            </h3>
            <button
              onClick={() => queryClient.invalidateQueries({ queryKey: ['sandboxes'] })}
              className="btn-ghost text-xs flex items-center gap-1"
            >
              <RefreshCw className="w-3 h-3" /> Refresh
            </button>
          </div>

          <table className="w-full">
            <thead>
              <tr className="border-b border-border-default">
                <th className="table-header py-2.5 px-4 text-left">Container</th>
                <th className="table-header py-2.5 px-4 text-left">Game</th>
                <th className="table-header py-2.5 px-4 text-left">Status</th>
                <th className="table-header py-2.5 px-4 text-right">CPU</th>
                <th className="table-header py-2.5 px-4 text-right">RAM</th>
                <th className="table-header py-2.5 px-4 text-center">VNC</th>
                <th className="table-header py-2.5 px-4 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {isLoading ? (
                <tr><td colSpan={7} className="py-8 text-center text-text-muted">Loading...</td></tr>
              ) : !containers?.length ? (
                <tr><td colSpan={7} className="py-12 text-center text-text-muted">
                  No sandboxes running. Start a farm session with Sandbox mode to launch game containers.
                </td></tr>
              ) : containers.map((c) => (
                <tr
                  key={c.id}
                  className={cn(
                    'border-b border-border-default hover:bg-bg-tertiary transition-colors',
                    selectedVNC === c.vnc_port && 'bg-accent/5'
                  )}
                >
                  <td className="py-2.5 px-4">
                    <div>
                      <p className="text-sm font-medium font-mono">{c.name}</p>
                      <p className="text-[11px] text-text-muted">{c.hostname}</p>
                    </div>
                  </td>
                  <td className="py-2.5 px-4">
                    <span className={cn(
                      'badge',
                      c.game_type === 'cs2' ? 'bg-game-cs2/15 text-game-cs2' : 'bg-game-dota2/15 text-game-dota2'
                    )}>
                      {c.game_type.toUpperCase()}
                    </span>
                  </td>
                  <td className="py-2.5 px-4">
                    <span className={cn(
                      'badge',
                      c.status === 'running' ? 'bg-status-active/15 text-status-active' : 'bg-status-idle/15 text-status-idle'
                    )}>
                      <span className={cn(
                        'w-1.5 h-1.5 rounded-full mr-1.5',
                        c.status === 'running' ? 'bg-status-active animate-pulse-slow' : 'bg-status-idle'
                      )} />
                      {c.status}
                    </span>
                  </td>
                  <td className="py-2.5 px-4 text-right font-mono text-sm">{c.cpu_percent.toFixed(1)}%</td>
                  <td className="py-2.5 px-4 text-right font-mono text-sm">{c.memory_mb} MB</td>
                  <td className="py-2.5 px-4 text-center">
                    <button
                      onClick={() => setSelectedVNC(c.vnc_port === selectedVNC ? null : c.vnc_port)}
                      className={cn(
                        'text-xs px-2 py-1 rounded',
                        selectedVNC === c.vnc_port ? 'bg-accent text-white' : 'btn-ghost'
                      )}
                    >
                      :{c.vnc_port}
                    </button>
                  </td>
                  <td className="py-2.5 px-4 text-right">
                    <button
                      onClick={() => stopMutation.mutate(c.account_id)}
                      className="btn-danger text-xs px-2 py-1"
                    >
                      <Square className="w-3 h-3" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* VNC Preview panel */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
            <Monitor className="w-4 h-4 text-accent" /> Game Preview
          </h3>
          {selectedVNC ? (() => {
            const selectedContainer = containers?.find(c => c.vnc_port === selectedVNC);
            const displayStr = selectedContainer?.display ?? ':100';
            return (
            <div className="space-y-2">
              <div className="bg-black rounded-lg border border-border-default aspect-[4/3] relative overflow-hidden">
                <VncViewer
                  key={selectedVNC}
                  port={selectedVNC}
                  viewOnly={true}
                  className="w-full h-full relative"
                />
              </div>
              <button
                onClick={() => setFullscreenVNC({ port: selectedVNC, display: displayStr })}
                className="w-full btn-primary text-sm py-2 flex items-center justify-center gap-2"
              >
                <Maximize2 className="w-4 h-4" />
                Open Interactive VNC
              </button>
              <p className="text-xs text-text-muted text-center font-mono">
                VNC :{selectedVNC} via WebSocket proxy
              </p>
            </div>
            );
          })() : (
            <div className="bg-bg-primary rounded-lg border border-border-default aspect-[4/3] flex items-center justify-center">
              <div className="text-center text-text-muted">
                <Monitor className="w-8 h-8 mx-auto mb-2 opacity-30" />
                <p className="text-sm">Select a container to preview</p>
                <p className="text-xs mt-1">Click VNC port in the table</p>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Autoplay Bot Panel */}
      <div className="card !p-0 overflow-hidden">
        <div className="px-4 py-3 border-b border-border-default flex items-center justify-between">
          <h3 className="text-sm font-semibold flex items-center gap-2">
            <Gamepad2 className="w-4 h-4 text-accent" /> CS2 Autoplay Bots
          </h3>
          <span className="text-xs text-text-muted">
            {botStatuses?.filter(b => b.running).length ?? 0} active
          </span>
        </div>

        {(!botStatuses || botStatuses.length === 0) ? (
          <div className="py-8 text-center text-text-muted text-sm">
            <Bot className="w-8 h-8 mx-auto mb-2 opacity-30" />
            <p>No autoplay bots running</p>
            <p className="text-xs mt-1">Bots start automatically when CS2 sandboxes launch</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 p-4">
            {botStatuses.map((bot) => (
              <div
                key={bot.display}
                className={cn(
                  'rounded-lg border p-3',
                  bot.running ? 'border-accent/30 bg-accent/5' : 'border-border-default bg-bg-secondary'
                )}
              >
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <Bot className={cn('w-4 h-4', bot.running ? 'text-accent' : 'text-text-muted')} />
                    <span className="text-sm font-mono font-medium">:{bot.display}</span>
                  </div>
                  <span className={cn(
                    'badge text-[11px]',
                    bot.phase === 'alive' ? 'bg-status-active/15 text-status-active' :
                    bot.phase === 'dead' ? 'bg-red-500/15 text-red-400' :
                    bot.phase === 'loading' ? 'bg-yellow-500/15 text-yellow-400' :
                    'bg-blue-500/15 text-blue-400'
                  )}>
                    {bot.phase}
                  </span>
                </div>

                <div className="grid grid-cols-3 gap-2 text-xs">
                  <div className="flex items-center gap-1 text-text-muted">
                    <Heart className="w-3 h-3" />
                    <span className={bot.health > 0 ? 'text-status-active' : 'text-red-400'}>
                      {bot.health}hp
                    </span>
                  </div>
                  <div className="flex items-center gap-1 text-text-muted">
                    <Clock className="w-3 h-3" />
                    <span>{bot.uptime || '—'}</span>
                  </div>
                  <div className="text-right text-text-muted">
                    K:{bot.kills} D:{bot.deaths}
                  </div>
                </div>

                {bot.map && (
                  <p className="text-[11px] text-text-muted mt-1 font-mono">{bot.map}</p>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Manual bot controls for containers without active bots */}
        {containers && containers.length > 0 && (
          <div className="px-4 py-2 border-t border-border-default flex items-center gap-2 flex-wrap">
            {containers.filter(c => c.game_type === 'cs2' && c.status === 'running').map(c => {
              const displayNum = parseInt(c.display?.replace(':', '') || '0', 10);
              const hasBot = botStatuses?.some(b => b.display === displayNum && b.running);
              return (
                <button
                  key={c.id}
                  onClick={() => hasBot ? apStopMutation.mutate(c.account_id) : apStartMutation.mutate(c.account_id)}
                  className={cn(
                    'text-xs px-3 py-1.5 rounded-md font-medium transition-colors',
                    hasBot
                      ? 'bg-red-500/15 text-red-400 hover:bg-red-500/25'
                      : 'bg-accent/15 text-accent hover:bg-accent/25'
                  )}
                >
                  {hasBot ? `Stop Bot ${c.display}` : `Start Bot ${c.display}`}
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
