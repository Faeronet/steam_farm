import { useState, useEffect, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useAccounts } from '@/hooks/useAccounts';
import { useFarmLogs } from '@/hooks/useFarmLogs';
import { startFarm, stopFarm } from '@/api/farm';
import StatusDot from '@/components/shared/StatusDot';
import Badge from '@/components/shared/Badge';
import { cn, formatDuration } from '@/lib/utils';
import { Play, Square, Gift, Terminal, Wifi, WifiOff, Trash2 } from 'lucide-react';

export default function Dota2Farm() {
  const { data: accounts, isLoading } = useAccounts('dota2');
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const { logs, connected, addLocal, clear } = useFarmLogs();
  const logRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();

  const startMutation = useMutation({
    mutationFn: (ids: number[]) => startFarm(ids, undefined, 'dota2'),
    onMutate: (ids) => {
      addLocal('info', `[UI] Starting farm for ${ids.length} account(s)...`);
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
      if (data?.results) {
        for (const r of data.results) {
          if (r.error) {
            addLocal('error', `[UI] Account #${r.account_id}: ${r.error}`);
          } else {
            addLocal('success', `[UI] Account #${r.account_id}: ${r.mode} — ${r.status || 'started'}`);
          }
        }
      }
    },
    onError: (err) => {
      addLocal('error', `[UI] Start failed: ${err}`);
    },
  });

  const stopMutation = useMutation({
    mutationFn: (ids: number[]) => stopFarm(ids),
    onMutate: (ids) => {
      addLocal('info', `[UI] Stopping ${ids.length} account(s)...`);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
      addLocal('success', '[UI] Stopped.');
    },
    onError: (err) => {
      addLocal('error', `[UI] Stop failed: ${err}`);
    },
  });

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: 'smooth' });
  }, [logs]);

  const toggleSelect = (id: number) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAll = () => {
    if (!accounts) return;
    if (selected.size === accounts.length) setSelected(new Set());
    else setSelected(new Set(accounts.map(a => a.id)));
  };

  const farmingCount = accounts?.filter(a => a.status === 'farming').length ?? 0;

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="card !p-3">
        <div className="flex flex-wrap items-center gap-2">
          <button
            className="btn-primary text-sm flex items-center gap-1.5"
            disabled={selected.size === 0 || startMutation.isPending}
            onClick={() => startMutation.mutate([...selected])}
          >
            <Play className="w-3.5 h-3.5" />
            {startMutation.isPending ? 'Starting...' : `Start (${selected.size})`}
          </button>
          <button
            className="btn-danger text-sm flex items-center gap-1.5"
            disabled={selected.size === 0 || stopMutation.isPending}
            onClick={() => stopMutation.mutate([...selected])}
          >
            <Square className="w-3.5 h-3.5" /> Stop
          </button>
          <button className="btn-secondary text-sm flex items-center gap-1.5" disabled>
            <Gift className="w-3.5 h-3.5" /> Claim Rewards
          </button>

          <div className="flex-1" />

          <div className="text-xs font-mono text-text-muted">
            Active: {farmingCount} | Selected: {selected.size} | Total: {accounts?.length ?? 0}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2 card !p-0 overflow-hidden">
          <div className="overflow-x-auto max-h-[500px]">
            <table className="w-full">
              <thead className="sticky top-0 bg-bg-secondary z-10">
                <tr className="border-b border-border-default">
                  <th className="table-header py-3 px-4 text-left w-10">
                    <input type="checkbox"
                      checked={selected.size === (accounts?.length ?? 0) && (accounts?.length ?? 0) > 0}
                      onChange={selectAll}
                      className="rounded border-border-default" />
                  </th>
                  <th className="table-header py-3 px-4 text-left">Username</th>
                  <th className="table-header py-3 px-4 text-left">Status</th>
                  <th className="table-header py-3 px-4 text-left">Mode</th>
                  <th className="table-header py-3 px-4 text-left">Hours</th>
                  <th className="table-header py-3 px-4 text-left">Last Drop</th>
                </tr>
              </thead>
              <tbody>
                {isLoading ? (
                  Array.from({ length: 3 }).map((_, i) => (
                    <tr key={i} className="border-b border-border-default">
                      <td colSpan={6} className="py-3 px-4">
                        <div className="h-4 bg-bg-tertiary rounded animate-pulse" />
                      </td>
                    </tr>
                  ))
                ) : !accounts?.length ? (
                  <tr>
                    <td colSpan={6} className="py-12 text-center text-text-muted">
                      No Dota 2 accounts. Add one in the Accounts page.
                    </td>
                  </tr>
                ) : (
                  accounts.map(acc => (
                    <tr key={acc.id}
                      className={cn(
                        'border-b border-border-default hover:bg-bg-tertiary transition-colors cursor-pointer',
                        selected.has(acc.id) && 'bg-accent/5'
                      )}
                      onClick={() => toggleSelect(acc.id)}
                    >
                      <td className="py-3 px-4">
                        <input type="checkbox"
                          checked={selected.has(acc.id)}
                          onChange={() => toggleSelect(acc.id)}
                          className="rounded border-border-default" />
                      </td>
                      <td className="py-3 px-4">
                        <div className="flex items-center gap-2">
                          <StatusDot status={acc.status} />
                          <span className="font-medium text-sm">{acc.username}</span>
                        </div>
                      </td>
                      <td className="py-3 px-4"><Badge status={acc.status} /></td>
                      <td className="py-3 px-4 text-sm text-text-secondary capitalize">{acc.farm_mode}</td>
                      <td className="py-3 px-4 font-mono text-sm">{formatDuration(acc.dota_hours)}</td>
                      <td className="py-3 px-4 text-sm text-text-muted">
                        {acc.last_drop_at ? new Date(acc.last_drop_at).toLocaleDateString() : '—'}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        <div className="card !p-0 overflow-hidden flex flex-col">
          <div className="px-4 py-2.5 border-b border-border-default flex items-center gap-2">
            <Terminal className="w-4 h-4 text-accent" />
            <span className="text-sm font-semibold">Server Log</span>
            <div className="flex-1" />
            {connected
              ? <Wifi className="w-3.5 h-3.5 text-status-active" />
              : <WifiOff className="w-3.5 h-3.5 text-status-error" />}
            <button onClick={clear} className="p-1 hover:bg-bg-tertiary rounded" title="Clear logs">
              <Trash2 className="w-3.5 h-3.5 text-text-muted" />
            </button>
          </div>
          <div ref={logRef} className="flex-1 overflow-y-auto p-3 space-y-0.5 max-h-[500px] min-h-[300px] font-mono text-xs bg-[#0a0e17]">
            {logs.length === 0 ? (
              <p className="text-text-muted py-4 text-center text-[11px]">
                Select accounts and click Start to begin farming.<br/>
                Server logs will appear here in real-time.
              </p>
            ) : logs.map((log, i) => (
              <div key={i} className={cn(
                'flex gap-2 py-0.5 px-1 rounded leading-relaxed',
                log.level === 'error' && 'text-red-400',
                log.level === 'success' && 'text-emerald-400',
                log.level === 'warning' && 'text-amber-400',
                log.level === 'info' && 'text-slate-400',
              )}>
                <span className="text-slate-600 shrink-0">{log.time}</span>
                <span className="break-all">{log.message}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
