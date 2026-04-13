import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/layout/Layout';
import Dashboard from './pages/Dashboard';
import CS2Farm from './pages/CS2Farm';
import Dota2Farm from './pages/Dota2Farm';
import Drops from './pages/Drops';
import Accounts from './pages/Accounts';
import SandboxMonitor from './pages/SandboxMonitor';
import Settings from './pages/Settings';

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
        <Route path="/sandbox" element={<SandboxMonitor />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
    </Routes>
  );
}
