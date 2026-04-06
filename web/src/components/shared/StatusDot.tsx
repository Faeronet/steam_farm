import { cn, getStatusColor } from '@/lib/utils';

interface StatusDotProps {
  status: string;
  pulse?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

export default function StatusDot({ status, pulse, size = 'md' }: StatusDotProps) {
  const sizes = { sm: 'w-1.5 h-1.5', md: 'w-2 h-2', lg: 'w-3 h-3' };
  return (
    <span
      className={cn(
        'rounded-full inline-block',
        sizes[size],
        getStatusColor(status),
        (pulse || status === 'farming') && 'animate-pulse-slow'
      )}
    />
  );
}
