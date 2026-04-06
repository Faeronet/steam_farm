const API_BASE = '/api';

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${url}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });

  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(error.error || 'Request failed');
  }

  return res.json();
}

export interface Account {
  id: number;
  username: string;
  game_type: 'cs2' | 'dota2';
  farm_mode: 'protocol' | 'sandbox';
  status: string;
  status_detail?: string;
  is_prime: boolean;
  cs2_level: number;
  cs2_xp: number;
  cs2_xp_needed: number;
  cs2_rank?: string;
  armory_stars: number;
  dota_hours: number;
  last_drop_at?: string;
  farmed_this_week: boolean;
  drop_collected: boolean;
  tags: string[];
  group_name?: string;
  shared_secret?: string;
  identity_secret?: string;
}

export interface Drop {
  id: number;
  account_id: number;
  game_type: string;
  item_name: string;
  item_type?: string;
  item_image_url?: string;
  market_price?: number;
  dropped_at: string;
  claimed: boolean;
  sent_to_trade: boolean;
  choice_options?: unknown;
  chosen_items?: unknown;
}

export interface DashboardStats {
  total_accounts: number;
  farming_accounts: number;
  drops_this_week: number;
  weekly_revenue: number;
  pending_rewards: number;
  active_sandboxes: number;
}

export interface FarmSession {
  id: number;
  name?: string;
  game_type: string;
  farm_mode: string;
  account_ids: number[];
  started_at: string;
  ended_at?: string;
  total_hours: number;
  drops_count: number;
  status: string;
}

export const api = {
  getHealth: () => request<{ status: string }>('/health'),

  getAccounts: (gameType?: string) =>
    request<Account[]>(`/accounts${gameType ? `?game_type=${gameType}` : ''}`),

  createAccount: (data: Partial<Account> & { password: string }) =>
    request<{ id: number }>('/accounts', { method: 'POST', body: JSON.stringify(data) }),

  deleteAccount: (id: number) =>
    request<void>(`/accounts/${id}`, { method: 'DELETE' }),

  getDashboardStats: () => request<DashboardStats>('/stats/dashboard'),

  getDrops: (gameType?: string, limit?: number) =>
    request<Drop[]>(`/drops?${gameType ? `game_type=${gameType}&` : ''}limit=${limit || 100}`),

  getPendingDrops: () => request<Drop[]>('/drops/pending'),

  claimDrop: (id: number, choices: string[]) =>
    request<void>(`/drops/${id}/claim`, { method: 'POST', body: JSON.stringify({ chosen_items: choices }) }),

  getSessions: () => request<FarmSession[]>('/farm/sessions'),

  createSession: (data: { game_type: string; farm_mode: string; account_ids: number[] }) =>
    request<{ id: number }>('/farm/sessions', { method: 'POST', body: JSON.stringify(data) }),

  stopSession: (id: number) =>
    request<void>(`/farm/sessions/${id}/stop`, { method: 'POST' }),

  getFarmStatus: () => request<{ total: number; farming: number; idle: number; errored: number }>('/farm/status'),
};
