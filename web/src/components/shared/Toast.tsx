import { useEffect, useState } from 'react';
import { cn } from '@/lib/utils';
import { X, CheckCircle, AlertTriangle, Info, XCircle } from 'lucide-react';

type ToastType = 'success' | 'error' | 'warning' | 'info';

interface ToastItem {
  id: string;
  type: ToastType;
  message: string;
  duration?: number;
}

const toastIcons = {
  success: CheckCircle,
  error: XCircle,
  warning: AlertTriangle,
  info: Info,
};

const toastColors = {
  success: 'border-status-active bg-status-active/5',
  error: 'border-status-error bg-status-error/5',
  warning: 'border-status-queued bg-status-queued/5',
  info: 'border-accent bg-accent/5',
};

let addToastFn: ((toast: Omit<ToastItem, 'id'>) => void) | null = null;

export function toast(type: ToastType, message: string, duration = 5000) {
  addToastFn?.({ type, message, duration });
}

export default function ToastContainer() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  useEffect(() => {
    addToastFn = (t) => {
      const id = Math.random().toString(36).slice(2);
      setToasts((prev) => [...prev, { ...t, id }]);
      setTimeout(() => {
        setToasts((prev) => prev.filter((toast) => toast.id !== id));
      }, t.duration || 5000);
    };
    return () => { addToastFn = null; };
  }, []);

  const remove = (id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  };

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm">
      {toasts.map((t) => {
        const Icon = toastIcons[t.type];
        return (
          <div
            key={t.id}
            className={cn(
              'flex items-start gap-3 p-3 rounded-lg border animate-slide-in',
              'bg-bg-elevated backdrop-blur-sm',
              toastColors[t.type]
            )}
          >
            <Icon className="w-4 h-4 mt-0.5 shrink-0" />
            <p className="text-sm flex-1">{t.message}</p>
            <button onClick={() => remove(t.id)} className="text-text-muted hover:text-text-primary">
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
