export async function startFarm(
  accountIds: number[],
  mode?: 'protocol' | 'sandbox',
  gameType?: string
) {
  const body: Record<string, unknown> = { account_ids: accountIds };
  if (mode) body.mode = mode;
  if (gameType) body.game_type = gameType;
  const res = await fetch('/api/farm/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function stopFarm(accountIds: number[]) {
  const res = await fetch('/api/farm/stop', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account_ids: accountIds }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function stopAllFarm() {
  const res = await fetch('/api/farm/stop-all', { method: 'POST' });
  return res.json();
}

export async function getFarmStatus() {
  const res = await fetch('/api/farm/status');
  return res.json();
}
