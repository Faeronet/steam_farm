import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { stopAllFarm } from '@/api/farm';
import StatCard from '@/components/shared/StatCard';
import { Users, Activity, Gift, DollarSign, Bell, Server } from 'lucide-react';

function QuickActions() {
  const queryClient = useQueryClient();
  const startCS2 = useMutation({
    mutationFn: async () => {
      const accs = await api.getAccounts('cs2');
      const ids = accs.filter((a: { status: string }) => a.status === 'idle' || a.status === 'ready').map((a: { id: number }) => a.id);
      if (ids.length === 0) return;
      return fetch('/api/farm/start', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ account_ids: ids, mode: 'sandbox', game_type: 'cs2' }),
      }).then(r => r.json());
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['accounts'] }),
  });
  const startDota = useMutation({
    mutationFn: async () => {
      const accs = await api.getAccounts('dota2');
      const ids = accs.filter((a: { status: string }) => a.status === 'idle' || a.status === 'ready').map((a: { id: number }) => a.id);
      if (ids.length === 0) return;
      return fetch('/api/farm/start', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ account_ids: ids, mode: 'protocol', game_type: 'dota2' }),
      }).then(r => r.json());
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['accounts'] }),
  });
  const stopAll = useMutation({
    mutationFn: () => stopAllFarm(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['accounts'] }),
  });

  return (
    <div className="space-y-2">
      <button className="btn-primary w-full text-sm" onClick={() => startCS2.mutate()} disabled={startCS2.isPending}>
        {startCS2.isPending ? 'Starting...' : 'Start All CS2'}
      </button>
      <button className="btn-primary w-full text-sm" onClick={() => startDota.mutate()} disabled={startDota.isPending}>
        {startDota.isPending ? 'Starting...' : 'Start All Dota 2'}
      </button>
      <button className="btn-danger w-full text-sm" onClick={() => stopAll.mutate()} disabled={stopAll.isPending}>
        {stopAll.isPending ? 'Stopping...' : 'Stop All'}
      </button>
    </div>
  );
}

export default function Dashboard() {
  const { data: stats } = useQuery({
    queryKey: ['dashboard-stats'],
    queryFn: api.getDashboardStats,
    refetchInterval: 5000,
  });

  const { data: drops } = useQuery({
    queryKey: ['drops-recent'],
    queryFn: () => api.getDrops(undefined, 20),
  });

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Total Accounts"
          value={stats?.total_accounts ?? 0}
          icon={Users}
        />
        <StatCard
          title="Farming Now"
          value={stats?.farming_accounts ?? 0}
          icon={Activity}
        />
        <StatCard
          title="Drops This Week"
          value={stats?.drops_this_week ?? 0}
          icon={Gift}
        />
        <StatCard
          title="Weekly Revenue"
          value={`$${(stats?.weekly_revenue ?? 0).toFixed(2)}`}
          icon={DollarSign}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2 card">
          <h3 className="text-sm font-semibold mb-4 flex items-center gap-2">
            <Activity className="w-4 h-4 text-accent" />
            Live Activity Feed
          </h3>
          <div className="space-y-2 max-h-[400px] overflow-y-auto">
            {(!drops || drops.length === 0) ? (
              <p className="text-sm text-text-muted py-8 text-center">No recent activity</p>
            ) : (
              drops.map((drop) => (
                <div
                  key={drop.id}
                  className="flex items-center gap-3 py-2 px-3 rounded-lg hover:bg-bg-tertiary transition-colors"
                >
                  <Gift className="w-4 h-4 text-status-farmed shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">
                      <span className="text-text-secondary">Account #{drop.account_id}</span>
                      {' → '}
                      <span className="font-medium">{drop.item_name}</span>
                    </p>
                  </div>
                  <span className="text-[11px] font-mono text-text-muted shrink-0">
                    {new Date(drop.dropped_at).toLocaleTimeString()}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="space-y-4">
          <div className="card">
            <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
              <Bell className="w-4 h-4 text-accent" />
              Pending Rewards
            </h3>
            <div className="text-3xl font-bold text-status-farmed">
              {stats?.pending_rewards ?? 0}
            </div>
            <p className="text-xs text-text-muted mt-1">Rewards awaiting choice</p>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
              <Server className="w-4 h-4 text-accent" />
              Active Sandboxes
            </h3>
            <div className="text-3xl font-bold">{stats?.active_sandboxes ?? 0}</div>
            <p className="text-xs text-text-muted mt-1">Docker containers running</p>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">Quick Actions</h3>
            <QuickActions />
          </div>
        </div>
      </div>
    </div>
  );
}
