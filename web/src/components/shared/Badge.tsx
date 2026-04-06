import { cn, getStatusColor, getStatusLabel } from '@/lib/utils';

interface BadgeProps {
  status: string;
  pulse?: boolean;
}

export default function Badge({ status, pulse }: BadgeProps) {
  const colorMap: Record<string, string> = {
    idle: 'bg-status-idle/15 text-status-idle',
    ready: 'bg-status-ready/15 text-status-ready',
    farming: 'bg-status-active/15 text-status-active',
    queued: 'bg-status-queued/15 text-status-queued',
    farmed: 'bg-status-farmed/15 text-status-farmed',
    collected: 'bg-status-collected/15 text-status-collected',
    done: 'bg-status-done/15 text-status-done',
    error: 'bg-status-error/15 text-status-error',
    banned: 'bg-status-error/15 text-status-error',
  };

  return (
    <span className={cn('badge', colorMap[status] || 'bg-bg-tertiary text-text-secondary', pulse && 'animate-pulse-slow')}>
      <span className={cn('w-1.5 h-1.5 rounded-full mr-1.5', getStatusColor(status))} />
      {getStatusLabel(status)}
    </span>
  );
}
