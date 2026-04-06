import { cn, formatNumber } from '@/lib/utils';

interface XPProgressBarProps {
  current: number;
  needed: number;
  level: number;
  className?: string;
}

export default function XPProgressBar({ current, needed, level, className }: XPProgressBarProps) {
  const percent = needed > 0 ? Math.min((current / needed) * 100, 100) : 0;

  return (
    <div className={cn('flex items-center gap-3', className)}>
      <span className="text-xs font-mono text-text-secondary w-8 text-right">L{level}</span>
      <div className="flex-1 h-1.5 bg-bg-tertiary rounded-full overflow-hidden">
        <div
          className="h-full bg-gradient-to-r from-accent to-blue-400 rounded-full transition-all duration-500"
          style={{ width: `${percent}%` }}
        />
      </div>
      <span className="text-[11px] font-mono text-text-muted min-w-[80px] text-right">
        {formatNumber(current)} / {formatNumber(needed)}
      </span>
    </div>
  );
}
