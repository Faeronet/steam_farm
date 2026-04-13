import { lazy, Suspense } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/layout/Layout';
import Dashboard from './pages/Dashboard';
import CS2Farm from './pages/CS2Farm';
import Dota2Farm from './pages/Dota2Farm';
import Drops from './pages/Drops';
import Accounts from './pages/Accounts';
import Settings from './pages/Settings';

// novnc тянется только сюда — иначе ошибка pre-bundle ломала весь dev-бандл (белый экран).
const SandboxMonitor = lazy(() => import('./pages/SandboxMonitor'));

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/cs2" element={<CS2Farm />} />
        <Route path="/dota2" element={<Dota2Farm />} />
        <Route path="/drops" element={<Drops />} />
        <Route path="/accounts" element={<Accounts />} />
        <Route
          path="/sandbox"
          element={
            <Suspense fallback={<div className="p-8 text-muted-foreground">Загрузка…</div>}>
              <SandboxMonitor />
            </Suspense>
          }
        />
        <Route path="/settings" element={<Settings />} />
      </Route>
    </Routes>
  );
}
