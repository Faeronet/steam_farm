import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { cn } from '@/lib/utils';
import { Monitor, Play, Square, RefreshCw, Cpu, HardDrive } from 'lucide-react';

interface ContainerInfo {
  ID: string;
  Name: string;
  AccountID: number;
  GameType: string;
  Status: string;
  VNCPort: number;
  CPUPercent: number;
  MemoryMB: number;
  Hostname: string;
}

async function fetchSandboxes(): Promise<ContainerInfo[]> {
  const res = await fetch('/api/sandbox/list');
  return res.json();
}

async function launchSandbox(accountId: number, gameType: string) {
  const res = await fetch('/api/sandbox/launch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_id: accountId, game_type: gameType }),
  });
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

export default function SandboxMonitor() {
  const queryClient = useQueryClient();
  const [selectedVNC, setSelectedVNC] = useState<number | null>(null);

  const { data: containers, isLoading } = useQuery({
    queryKey: ['sandboxes'],
    queryFn: fetchSandboxes,
    refetchInterval: 3000,
  });

  const stopMutation = useMutation({
    mutationFn: (accountId: number) => stopSandbox(accountId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sandboxes'] }),
  });

  const totalCPU = containers?.reduce((s, c) => s + c.CPUPercent, 0) ?? 0;
  const totalRAM = containers?.reduce((s, c) => s + c.MemoryMB, 0) ?? 0;

  return (
    <div className="space-y-4 animate-fade-in">
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
            <p className="text-xl font-bold">{containers?.filter(c => c.Status === 'running').length ?? 0}</p>
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
                  key={c.ID}
                  className={cn(
                    'border-b border-border-default hover:bg-bg-tertiary transition-colors',
                    selectedVNC === c.VNCPort && 'bg-accent/5'
                  )}
                >
                  <td className="py-2.5 px-4">
                    <div>
                      <p className="text-sm font-medium font-mono">{c.Name}</p>
                      <p className="text-[11px] text-text-muted">{c.Hostname}</p>
                    </div>
                  </td>
                  <td className="py-2.5 px-4">
                    <span className={cn(
                      'badge',
                      c.GameType === 'cs2' ? 'bg-game-cs2/15 text-game-cs2' : 'bg-game-dota2/15 text-game-dota2'
                    )}>
                      {c.GameType.toUpperCase()}
                    </span>
                  </td>
                  <td className="py-2.5 px-4">
                    <span className={cn(
                      'badge',
                      c.Status === 'running' ? 'bg-status-active/15 text-status-active' : 'bg-status-idle/15 text-status-idle'
                    )}>
                      <span className={cn(
                        'w-1.5 h-1.5 rounded-full mr-1.5',
                        c.Status === 'running' ? 'bg-status-active animate-pulse-slow' : 'bg-status-idle'
                      )} />
                      {c.Status}
                    </span>
                  </td>
                  <td className="py-2.5 px-4 text-right font-mono text-sm">{c.CPUPercent.toFixed(1)}%</td>
                  <td className="py-2.5 px-4 text-right font-mono text-sm">{c.MemoryMB} MB</td>
                  <td className="py-2.5 px-4 text-center">
                    <button
                      onClick={() => setSelectedVNC(c.VNCPort === selectedVNC ? null : c.VNCPort)}
                      className={cn(
                        'text-xs px-2 py-1 rounded',
                        selectedVNC === c.VNCPort ? 'bg-accent text-white' : 'btn-ghost'
                      )}
                    >
                      :{c.VNCPort}
                    </button>
                  </td>
                  <td className="py-2.5 px-4 text-right">
                    <button
                      onClick={() => stopMutation.mutate(c.AccountID)}
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
          {selectedVNC ? (
            <div className="space-y-2">
              <div className="bg-bg-primary rounded-lg border border-border-default aspect-[4/3] flex items-center justify-center overflow-hidden">
                <iframe
                  src={`http://127.0.0.1:${selectedVNC + 100}`}
                  className="w-full h-full border-0"
                  title="noVNC Preview"
                />
              </div>
              <p className="text-xs text-text-muted text-center font-mono">
                VNC :{selectedVNC} → noVNC :{selectedVNC + 100}
              </p>
              <p className="text-[11px] text-text-muted text-center">
                Use a noVNC client or connect directly via VNC viewer to 127.0.0.1:{selectedVNC}
              </p>
            </div>
          ) : (
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
    </div>
  );
}
