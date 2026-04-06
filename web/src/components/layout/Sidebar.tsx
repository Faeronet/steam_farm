import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils';
import {
  LayoutDashboard,
  Crosshair,
  Swords,
  Gift,
  Users,
  Monitor,
  Settings,
  Zap,
} from 'lucide-react';

const navigation = [
  { name: 'Dashboard', href: '/dashboard', icon: LayoutDashboard },
  { name: 'CS2 Farm', href: '/cs2', icon: Crosshair, color: 'text-game-cs2' },
  { name: 'Dota 2 Farm', href: '/dota2', icon: Swords, color: 'text-game-dota2' },
  { name: 'Drops / Rewards', href: '/drops', icon: Gift },
  { name: 'Accounts', href: '/accounts', icon: Users },
  { name: 'Sandbox Monitor', href: '/sandbox', icon: Monitor },
  { name: 'Settings', href: '/settings', icon: Settings },
];

export default function Sidebar() {
  return (
    <aside className="w-[260px] h-screen flex flex-col bg-bg-secondary border-r border-border-default fixed left-0 top-0 z-40">
      <div className="flex items-center gap-3 px-5 py-5 border-b border-border-default">
        <div className="w-9 h-9 rounded-lg bg-accent/20 flex items-center justify-center">
          <Zap className="w-5 h-5 text-accent" />
        </div>
        <div>
          <h1 className="text-sm font-bold text-text-primary tracking-tight">STEAM FARM</h1>
          <p className="text-[10px] text-text-muted uppercase tracking-widest">Control Panel</p>
        </div>
      </div>

      <nav className="flex-1 py-3 px-3 space-y-0.5 overflow-y-auto">
        {navigation.map((item) => (
          <NavLink
            key={item.href}
            to={item.href}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-150',
                isActive
                  ? 'bg-accent/10 text-accent glow-blue'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-tertiary'
              )
            }
          >
            <item.icon className={cn('w-[18px] h-[18px]', item.color)} />
            <span>{item.name}</span>
          </NavLink>
        ))}
      </nav>

      <div className="px-4 py-3 border-t border-border-default">
        <div className="flex items-center justify-between text-[11px] text-text-muted">
          <span>v0.1.0</span>
          <div className="flex items-center gap-1.5">
            <span className="status-dot bg-status-active animate-pulse-slow" />
            <span>Online</span>
          </div>
        </div>
      </div>
    </aside>
  );
}
