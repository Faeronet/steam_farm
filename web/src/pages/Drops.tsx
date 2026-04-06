import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { cn } from '@/lib/utils';
import { Gift, Check, AlertCircle } from 'lucide-react';

type TabType = 'all' | 'cs2' | 'dota2' | 'pending';

export default function Drops() {
  const [tab, setTab] = useState<TabType>('all');
  const queryClient = useQueryClient();

  const { data: allDrops } = useQuery({
    queryKey: ['drops', tab],
    queryFn: () => {
      if (tab === 'pending') return api.getPendingDrops();
      const gameType = tab === 'all' ? undefined : tab;
      return api.getDrops(gameType, 200);
    },
  });

  const claimMutation = useMutation({
    mutationFn: ({ id, choices }: { id: number; choices: string[] }) =>
      api.claimDrop(id, choices),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['drops'] }),
  });

  const tabs: { key: TabType; label: string; count?: number }[] = [
    { key: 'all', label: 'All Drops' },
    { key: 'cs2', label: 'CS2' },
    { key: 'dota2', label: 'Dota 2' },
    { key: 'pending', label: 'Pending', count: allDrops?.filter((d) => !d.claimed && d.choice_options).length },
  ];

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-center gap-1 bg-bg-secondary rounded-lg p-1 w-fit">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={cn(
              'px-4 py-2 rounded-md text-sm font-medium transition-colors',
              tab === t.key ? 'bg-accent text-white' : 'text-text-secondary hover:text-text-primary'
            )}
          >
            {t.label}
            {t.count !== undefined && t.count > 0 && (
              <span className="ml-1.5 bg-status-farmed/20 text-status-farmed text-xs px-1.5 py-0.5 rounded-full animate-pulse-slow">
                {t.count}
              </span>
            )}
          </button>
        ))}
      </div>

      {tab === 'pending' ? (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {!allDrops?.length ? (
            <div className="card col-span-full py-12 text-center">
              <AlertCircle className="w-8 h-8 text-text-muted mx-auto mb-2" />
              <p className="text-text-muted">No pending rewards</p>
            </div>
          ) : (
            allDrops.map((drop) => (
              <div key={drop.id} className="card border-status-farmed/30">
                <div className="flex items-center gap-2 mb-3">
                  <Gift className="w-5 h-5 text-status-farmed" />
                  <span className="font-semibold text-sm">Account #{drop.account_id}</span>
                  <span className="badge bg-status-farmed/15 text-status-farmed animate-pulse-slow">
                    REWARD AVAILABLE
                  </span>
                </div>
                <p className="text-sm text-text-secondary mb-3">{drop.item_name}</p>
                <div className="flex gap-2">
                  <button
                    onClick={() => claimMutation.mutate({ id: drop.id, choices: [drop.item_name] })}
                    className="btn-primary text-sm flex items-center gap-1.5"
                  >
                    <Check className="w-3.5 h-3.5" /> Claim
                  </button>
                </div>
              </div>
            ))
          )}
        </div>
      ) : (
        <div className="card !p-0 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border-default">
                <th className="table-header py-3 px-4 text-left">Time</th>
                <th className="table-header py-3 px-4 text-left">Account</th>
                <th className="table-header py-3 px-4 text-left">Game</th>
                <th className="table-header py-3 px-4 text-left">Item</th>
                <th className="table-header py-3 px-4 text-left">Type</th>
                <th className="table-header py-3 px-4 text-right">Price</th>
                <th className="table-header py-3 px-4 text-center">Status</th>
              </tr>
            </thead>
            <tbody>
              {!allDrops?.length ? (
                <tr>
                  <td colSpan={7} className="py-12 text-center text-text-muted">No drops yet</td>
                </tr>
              ) : (
                allDrops.map((drop) => (
                  <tr key={drop.id} className="border-b border-border-default hover:bg-bg-tertiary transition-colors">
                    <td className="py-3 px-4 text-sm font-mono text-text-muted">
                      {new Date(drop.dropped_at).toLocaleString()}
                    </td>
                    <td className="py-3 px-4 text-sm">#{drop.account_id}</td>
                    <td className="py-3 px-4">
                      <span className={cn(
                        'badge',
                        drop.game_type === 'cs2' ? 'bg-game-cs2/15 text-game-cs2' : 'bg-game-dota2/15 text-game-dota2'
                      )}>
                        {drop.game_type.toUpperCase()}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-sm font-medium">{drop.item_name}</td>
                    <td className="py-3 px-4 text-sm text-text-secondary">{drop.item_type || '—'}</td>
                    <td className="py-3 px-4 text-sm text-right font-mono">
                      {drop.market_price ? `$${drop.market_price.toFixed(2)}` : '—'}
                    </td>
                    <td className="py-3 px-4 text-center">
                      {drop.claimed ? (
                        <span className="badge bg-status-active/15 text-status-active">Claimed</span>
                      ) : (
                        <span className="badge bg-status-queued/15 text-status-queued">Pending</span>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
