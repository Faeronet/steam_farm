import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatNumber(n: number): string {
  return new Intl.NumberFormat().format(n);
}

export function formatDuration(hours: number): string {
  const h = Math.floor(hours);
  const m = Math.floor((hours - h) * 60);
  return `${h}h ${m}m`;
}

export function getStatusColor(status: string): string {
  const colors: Record<string, string> = {
    idle: 'bg-status-idle',
    ready: 'bg-status-ready',
    farming: 'bg-status-active',
    queued: 'bg-status-queued',
    farmed: 'bg-status-farmed',
    collected: 'bg-status-collected',
    done: 'bg-status-done',
    error: 'bg-status-error',
    banned: 'bg-status-error',
  };
  return colors[status] || 'bg-status-idle';
}

export function getStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    idle: 'Idle',
    ready: 'Ready',
    farming: 'Farming',
    queued: 'Queued',
    farmed: 'Farmed',
    collected: 'Collected',
    done: 'Done',
    error: 'Error',
    banned: 'Banned',
  };
  return labels[status] || status;
}
