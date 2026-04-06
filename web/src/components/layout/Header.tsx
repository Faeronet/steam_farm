import { useLocation } from 'react-router-dom';
import { Bell, Search } from 'lucide-react';

const titles: Record<string, string> = {
  '/dashboard': 'Dashboard',
  '/cs2': 'CS2 Farm',
  '/dota2': 'Dota 2 Farm',
  '/drops': 'Drops & Rewards',
  '/accounts': 'Accounts',
  '/sandbox': 'Sandbox Monitor',
  '/settings': 'Settings',
};

export default function Header() {
  const location = useLocation();
  const title = titles[location.pathname] || 'Steam Farm';

  return (
    <header className="h-14 border-b border-border-default flex items-center justify-between px-6 bg-bg-secondary/50 backdrop-blur-sm sticky top-0 z-30">
      <div className="flex items-center gap-4">
        <h2 className="text-lg font-semibold">{title}</h2>
      </div>

      <div className="flex items-center gap-3">
        <button className="flex items-center gap-2 px-3 py-1.5 bg-bg-tertiary rounded-lg text-sm text-text-secondary hover:text-text-primary transition-colors">
          <Search className="w-4 h-4" />
          <span className="hidden sm:inline">Search...</span>
          <kbd className="hidden sm:inline text-[10px] bg-bg-primary px-1.5 py-0.5 rounded text-text-muted">
            Ctrl+K
          </kbd>
        </button>

        <button className="relative p-2 rounded-lg hover:bg-bg-tertiary transition-colors">
          <Bell className="w-4 h-4 text-text-secondary" />
          <span className="absolute top-1 right-1 w-2 h-2 bg-accent rounded-full" />
        </button>

        <div className="flex items-center gap-1.5 text-xs text-text-secondary">
          <span className="status-dot bg-status-active animate-pulse-slow" />
          <span>Connected</span>
        </div>
      </div>
    </header>
  );
}
