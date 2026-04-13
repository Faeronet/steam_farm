import { useState, useEffect, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useAccounts } from '@/hooks/useAccounts';
import { useFarmLogs } from '@/hooks/useFarmLogs';
import { useYoloLogs } from '@/hooks/useYoloLogs';
import { useSigScanLogs } from '@/hooks/useSigScanLogs';
import { useCS2MemLatestByDisplay } from '@/hooks/useCS2MemTelemetry';
import { startFarm, stopFarm } from '@/api/farm';
import StatusDot from '@/components/shared/StatusDot';
import Badge from '@/components/shared/Badge';
import XPProgressBar from '@/components/shared/XPProgressBar';
import { cn } from '@/lib/utils';
import {
  Play, Square, Gift, Send, ShieldAlert, Terminal, Wifi, WifiOff, Trash2, Brain, Cpu, ScanSearch,
} from 'lucide-react';

export default function CS2Farm() {
  const { data: accounts, isLoading } = useAccounts('cs2');
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const { logs, connected, addLocal, clear } = useFarmLogs();
  const { logs: yoloLogs, connected: yoloWS, clear: clearYolo } = useYoloLogs();
  const { logs: sigScanLogs, connected: sigScanWS, clear: clearSigScan } = useSigScanLogs();
  const memByDisp = useCS2MemLatestByDisplay();
  const logRef = useRef<HTMLDivElement>(null);
  const yoloLogRef = useRef<HTMLDivElement>(null);
  const sigScanLogRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();

  const startMutation = useMutation({
    mutationFn: (ids: number[]) => startFarm(ids, undefined, 'cs2'),
    onMutate: (ids) => addLocal('info', `[UI] Starting farm for ${ids.length} CS2 account(s)...`),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
      if (data?.results) {
        for (const r of data.results) {
          if (r.error) addLocal('error', `[UI] Account #${r.account_id}: ${r.error}`);
          else addLocal('success', `[UI] Account #${r.account_id}: ${r.mode} — ${r.status || 'started'}`);
        }
      }
    },
    onError: (err) => addLocal('error', `[UI] Start failed: ${err}`),
  });

  const stopMutation = useMutation({
    mutationFn: (ids: number[]) => stopFarm(ids),
    onMutate: (ids) => addLocal('info', `[UI] Stopping ${ids.length} CS2 account(s)...`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
      addLocal('success', '[UI] Stopped.');
    },
    onError: (err) => addLocal('error', `[UI] Stop failed: ${err}`),
  });

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: 'smooth' });
  }, [logs]);

  useEffect(() => {
    yoloLogRef.current?.scrollTo({ top: yoloLogRef.current.scrollHeight, behavior: 'smooth' });
  }, [yoloLogs]);

  useEffect(() => {
    sigScanLogRef.current?.scrollTo({ top: sigScanLogRef.current.scrollHeight, behavior: 'smooth' });
  }, [sigScanLogs]);

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

  const selectUnfarmed = () => {
    if (!accounts) return;
    const unfarmed = accounts.filter(a => !a.farmed_this_week).slice(0, 10);
    setSelected(new Set(unfarmed.map(a => a.id)));
  };

  const farmingCount = accounts?.filter(a => a.status === 'farming').length ?? 0;
  const farmedCount = accounts?.filter(a => a.farmed_this_week).length ?? 0;
  const totalCount = accounts?.length ?? 0;

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
            <Gift className="w-3.5 h-3.5" /> Collect
          </button>
          <button className="btn-secondary text-sm flex items-center gap-1.5" disabled>
            <Send className="w-3.5 h-3.5" /> Loot
          </button>
          <button className="btn-ghost text-sm flex items-center gap-1.5" disabled>
            <ShieldAlert className="w-3.5 h-3.5" /> Ban Check
          </button>

          <div className="flex-1" />

          <button onClick={selectUnfarmed} className="btn-ghost text-sm">
            Select 10 Unfarmed
          </button>

          <div className="text-xs font-mono text-text-muted">
            Farmed: {farmedCount}/{totalCount} | Active: {farmingCount}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-4 gap-4">
        <div className="xl:col-span-3 card !p-0 overflow-hidden">
          <div className="overflow-x-auto max-h-[500px]">
            <table className="w-full">
              <thead className="sticky top-0 bg-bg-secondary z-10">
                <tr className="border-b border-border-default">
                  <th className="table-header py-3 px-4 text-left w-10">
                    <input type="checkbox"
                      checked={selected.size === totalCount && totalCount > 0}
                      onChange={selectAll}
                      className="rounded border-border-default" />
                  </th>
                  <th className="table-header py-3 px-4 text-left">Username</th>
                  <th className="table-header py-3 px-4 text-left">Status</th>
                  <th className="table-header py-3 px-4 text-left">Level</th>
                  <th className="table-header py-3 px-4 text-left min-w-[200px]">XP Progress</th>
                  <th className="table-header py-3 px-4 text-center">Prime</th>
                  <th className="table-header py-3 px-4 text-left">Rank</th>
                  <th className="table-header py-3 px-4 text-center">Stars</th>
                  <th className="table-header py-3 px-4 text-left">Last Drop</th>
                </tr>
              </thead>
              <tbody>
                {isLoading ? (
                  Array.from({ length: 5 }).map((_, i) => (
                    <tr key={i} className="border-b border-border-default">
                      <td colSpan={9} className="py-3 px-4">
                        <div className="h-4 bg-bg-tertiary rounded animate-pulse" />
                      </td>
                    </tr>
                  ))
                ) : !accounts?.length ? (
                  <tr>
                    <td colSpan={9} className="py-12 text-center text-text-muted">
                      No CS2 accounts. Add one in the Accounts page.
                    </td>
                  </tr>
                ) : (
                  accounts.map(acc => (
                    <tr key={acc.id}
                      className={cn(
                        'border-b border-border-default hover:bg-bg-tertiary transition-colors cursor-pointer',
                        selected.has(acc.id) && 'bg-accent/5',
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
                      <td className="py-3 px-4"><Badge status={acc.status} pulse={acc.status === 'farmed'} /></td>
                      <td className="py-3 px-4 font-mono text-sm">{acc.cs2_level}</td>
                      <td className="py-3 px-4">
                        <XPProgressBar current={acc.cs2_xp} needed={acc.cs2_xp_needed} level={acc.cs2_level} />
                      </td>
                      <td className="py-3 px-4 text-center">
                        {acc.is_prime
                          ? <span className="text-status-active">✓</span>
                          : <span className="text-text-muted">✗</span>}
                      </td>
                      <td className="py-3 px-4 text-sm text-text-secondary">{acc.cs2_rank || '—'}</td>
                      <td className="py-3 px-4 text-center font-mono text-sm">{acc.armory_stars}</td>
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

        <div className="flex flex-col gap-4 min-w-0">
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
            <div ref={logRef} className="flex-1 overflow-y-auto p-3 space-y-0.5 max-h-[280px] min-h-[200px] font-mono text-xs bg-[#0a0e17]">
              {logs.length === 0 ? (
                <p className="text-text-muted py-4 text-center text-[11px]">
                  Select accounts and click Start.<br/>
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

          <div className="card !p-0 overflow-hidden flex flex-col">
            <div className="px-4 py-2.5 border-b border-border-default flex items-center gap-2">
              <Cpu className="w-4 h-4 text-cyan-400" />
              <span className="text-sm font-semibold">Память / ESP</span>
            </div>
            <div className="overflow-y-auto p-3 space-y-2 max-h-[200px] min-h-[120px] font-mono text-[11px] bg-[#071216]">
              {Object.keys(memByDisp).length === 0 ? (
                <p className="text-text-muted text-center leading-relaxed">
                  Снимки с каждого тика бота (~64/s) по WebSocket <code className="text-cyan-300/90">cs2:mem</code>:
                  опрос памяти, ESP-боксы, длина маршрута. Если пусто — бот не запущен или нет подключения WS.
                </p>
              ) : (
                [...Object.entries(memByDisp)].sort(([a], [b]) => Number(a) - Number(b)).map(([disp, m]) => (
                  <div key={disp} className="rounded border border-border-default/60 bg-bg-secondary/40 px-2 py-1.5 space-y-0.5">
                    <div className="flex flex-wrap gap-x-2 gap-y-0.5 text-slate-300">
                      <span className="text-cyan-300/90">:{disp}</span>
                      <span>tick {m.bot_tick}</span>
                      <span className={m.mem_poll_ran ? 'text-emerald-400' : 'text-slate-500'}>
                        poll {m.mem_poll_ran ? 'on' : 'off'}
                      </span>
                      <span className={m.mem_poll_ok ? 'text-emerald-400' : 'text-amber-400'}>
                        {m.mem_poll_ok ? 'read OK' : 'read fail'}
                      </span>
                      {m.mem_driver ? null : <span className="text-red-400">no driver</span>}
                    </div>
                    <div className="text-slate-500 break-all">
                      ESP boxes: <span className="text-slate-300">{m.esp_box_count}</span>
                      {' · '}nav {m.nav_route_step}/{m.nav_route_len}
                      {' · '}phase {m.phase}
                      {m.mem_poll_err ? (
                        <span className="text-red-400/90"> · {m.mem_poll_err}</span>
                      ) : null}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="card !p-0 overflow-hidden flex flex-col">
            <div className="px-4 py-2.5 border-b border-border-default flex items-center gap-2">
              <Brain className="w-4 h-4 text-violet-400" />
              <span className="text-sm font-semibold">YOLO / нейросеть</span>
              <div className="flex-1" />
              {yoloWS
                ? <Wifi className="w-3.5 h-3.5 text-status-active" />
                : <WifiOff className="w-3.5 h-3.5 text-status-error" />}
              <button onClick={clearYolo} className="p-1 hover:bg-bg-tertiary rounded" title="Clear">
                <Trash2 className="w-3.5 h-3.5 text-text-muted" />
              </button>
            </div>
            <div ref={yoloLogRef} className="flex-1 overflow-y-auto p-3 space-y-0.5 max-h-[260px] min-h-[180px] font-mono text-xs bg-[#0d0814]">
              {yoloLogs.length === 0 ? (
                <p className="text-text-muted py-4 text-center text-[11px]">
                  Логи <code className="text-violet-300/90">yolo_worker</code> и обмена бота (живой X11 ROI, не файлы).
                  <br />
                  Превью с боксами в VNC: <code className="text-violet-300/90">SFARM_YOLO_PREVIEW=1</code> при запуске desktop.
                </p>
              ) : yoloLogs.map((log, i) => (
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

          <div className="card !p-0 overflow-hidden flex flex-col">
            <div className="px-4 py-2.5 border-b border-border-default flex items-center gap-2">
              <ScanSearch className="w-4 h-4 text-cyan-400" />
              <span className="text-sm font-semibold">SigScanner (libclient.so)</span>
              <div className="flex-1" />
              {sigScanWS
                ? <Wifi className="w-3.5 h-3.5 text-status-active" />
                : <WifiOff className="w-3.5 h-3.5 text-status-error" />}
              <button onClick={clearSigScan} className="p-1 hover:bg-bg-tertiary rounded" title="Clear">
                <Trash2 className="w-3.5 h-3.5 text-text-muted" />
              </button>
            </div>
            <div ref={sigScanLogRef} className="flex-1 overflow-y-auto p-3 space-y-0.5 max-h-[260px] min-h-[180px] font-mono text-xs bg-[#0a1018]">
              {sigScanLogs.length === 0 ? (
                <p className="text-text-muted py-4 text-center text-[11px]">
                  Runtime signature scanner: ищет <code className="text-cyan-300/90">dw_*</code> в .text libclient.so при подключении к CS2.
                  <br />
                  Файл лога: <code className="text-cyan-300/90">/tmp/sfarm_sigscan.log</code>
                </p>
              ) : sigScanLogs.map((log, i) => (
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
    </div>
  );
}
