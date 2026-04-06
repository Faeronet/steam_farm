import { useState } from 'react';
import { useAccounts, useCreateAccount, useDeleteAccount } from '@/hooks/useAccounts';
import StatusDot from '@/components/shared/StatusDot';
import Badge from '@/components/shared/Badge';
import { cn } from '@/lib/utils';
import { Plus, Trash2, Upload, X } from 'lucide-react';

export default function Accounts() {
  const { data: accounts, isLoading } = useAccounts();
  const createMutation = useCreateAccount();
  const deleteMutation = useDeleteAccount();
  const [showAdd, setShowAdd] = useState(false);
  const [form, setForm] = useState({
    username: '',
    password: '',
    shared_secret: '',
    identity_secret: '',
    game_type: 'cs2' as 'cs2' | 'dota2',
    farm_mode: 'sandbox' as 'protocol' | 'sandbox',
    proxy: '',
    group_name: '',
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate(form, {
      onSuccess: () => {
        setShowAdd(false);
        setForm({ username: '', password: '', shared_secret: '', identity_secret: '', game_type: 'cs2', farm_mode: 'sandbox', proxy: '', group_name: '' });
      },
    });
  };

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h3 className="text-sm font-semibold text-text-secondary">
            {accounts?.length ?? 0} accounts
          </h3>
        </div>
        <div className="flex items-center gap-2">
          <button className="btn-ghost text-sm flex items-center gap-1.5">
            <Upload className="w-3.5 h-3.5" /> Import maFiles
          </button>
          <button onClick={() => setShowAdd(!showAdd)} className="btn-primary text-sm flex items-center gap-1.5">
            <Plus className="w-3.5 h-3.5" /> Add Account
          </button>
        </div>
      </div>

      {showAdd && (
        <form onSubmit={handleSubmit} className="card animate-fade-in">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold text-sm">Add New Account</h3>
            <button type="button" onClick={() => setShowAdd(false)} className="text-text-muted hover:text-text-primary">
              <X className="w-4 h-4" />
            </button>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {[
              { key: 'username', label: 'Username', required: true },
              { key: 'password', label: 'Password', required: true, type: 'password' },
              { key: 'shared_secret', label: 'Shared Secret' },
              { key: 'identity_secret', label: 'Identity Secret' },
              { key: 'proxy', label: 'Proxy (socks5://...)' },
              { key: 'group_name', label: 'Group' },
            ].map((field) => (
              <div key={field.key}>
                <label className="block text-xs text-text-muted mb-1">{field.label}</label>
                <input
                  type={field.type || 'text'}
                  value={(form as Record<string, string>)[field.key]}
                  onChange={(e) => setForm({ ...form, [field.key]: e.target.value })}
                  required={field.required}
                  className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                             focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50 transition-colors"
                />
              </div>
            ))}
            <div>
              <label className="block text-xs text-text-muted mb-1">Game</label>
              <select
                value={form.game_type}
                onChange={(e) => setForm({ ...form, game_type: e.target.value as 'cs2' | 'dota2' })}
                className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                           focus:border-accent focus:outline-none"
              >
                <option value="cs2">CS2</option>
                <option value="dota2">Dota 2</option>
              </select>
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Farm Mode</label>
              <select
                value={form.farm_mode}
                onChange={(e) => setForm({ ...form, farm_mode: e.target.value as 'protocol' | 'sandbox' })}
                className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                           focus:border-accent focus:outline-none"
              >
                <option value="sandbox">Sandbox (Docker)</option>
                <option value="protocol">Protocol (Lightweight)</option>
              </select>
            </div>
          </div>
          <div className="flex justify-end mt-4">
            <button type="submit" className="btn-primary text-sm" disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Adding...' : 'Add Account'}
            </button>
          </div>
        </form>
      )}

      <div className="card !p-0 overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="border-b border-border-default">
              <th className="table-header py-3 px-4 text-left">Username</th>
              <th className="table-header py-3 px-4 text-left">Game</th>
              <th className="table-header py-3 px-4 text-left">Mode</th>
              <th className="table-header py-3 px-4 text-left">Status</th>
              <th className="table-header py-3 px-4 text-left">Group</th>
              <th className="table-header py-3 px-4 text-center">2FA</th>
              <th className="table-header py-3 px-4 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <tr key={i} className="border-b border-border-default">
                  <td colSpan={7} className="py-3 px-4">
                    <div className="h-4 bg-bg-tertiary rounded animate-pulse" />
                  </td>
                </tr>
              ))
            ) : !accounts?.length ? (
              <tr>
                <td colSpan={7} className="py-12 text-center text-text-muted">No accounts yet</td>
              </tr>
            ) : (
              accounts.map((acc) => (
                <tr key={acc.id} className="border-b border-border-default hover:bg-bg-tertiary transition-colors">
                  <td className="py-3 px-4">
                    <div className="flex items-center gap-2">
                      <StatusDot status={acc.status} />
                      <span className="font-medium text-sm">{acc.username}</span>
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    <span className={cn(
                      'badge',
                      acc.game_type === 'cs2' ? 'bg-game-cs2/15 text-game-cs2' : 'bg-game-dota2/15 text-game-dota2'
                    )}>
                      {acc.game_type.toUpperCase()}
                    </span>
                  </td>
                  <td className="py-3 px-4 text-sm text-text-secondary capitalize">{acc.farm_mode}</td>
                  <td className="py-3 px-4"><Badge status={acc.status} /></td>
                  <td className="py-3 px-4 text-sm text-text-secondary">{acc.group_name || '—'}</td>
                  <td className="py-3 px-4 text-center">
                    {acc.shared_secret ? (
                      <span className="text-status-active text-sm">✓</span>
                    ) : (
                      <span className="text-text-muted text-sm">✗</span>
                    )}
                  </td>
                  <td className="py-3 px-4 text-right">
                    <button
                      onClick={() => deleteMutation.mutate(acc.id)}
                      className="p-1.5 rounded-lg hover:bg-status-error/10 text-text-muted hover:text-status-error transition-colors"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
