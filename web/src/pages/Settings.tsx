import { useState } from 'react';
import { Save } from 'lucide-react';

export default function Settings() {
  const [settings, setSettings] = useState({
    serverUrl: 'http://localhost:8080',
    tradeLink: '',
    telegramToken: '',
    telegramChatId: '',
    maxSandboxes: 10,
    autoCollect: true,
    autoLoot: false,
    replaceOnError: true,
  });

  return (
    <div className="max-w-2xl space-y-6 animate-fade-in">
      <div className="card">
        <h3 className="font-semibold text-sm mb-4">Server Configuration</h3>
        <div className="space-y-3">
          <div>
            <label className="block text-xs text-text-muted mb-1">Server URL</label>
            <input
              type="text"
              value={settings.serverUrl}
              onChange={(e) => setSettings({ ...settings, serverUrl: e.target.value })}
              className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                         focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50"
            />
          </div>
          <div>
            <label className="block text-xs text-text-muted mb-1">Trade Link</label>
            <input
              type="text"
              value={settings.tradeLink}
              onChange={(e) => setSettings({ ...settings, tradeLink: e.target.value })}
              placeholder="https://steamcommunity.com/tradeoffer/new/?partner=..."
              className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                         focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50"
            />
          </div>
        </div>
      </div>

      <div className="card">
        <h3 className="font-semibold text-sm mb-4">Telegram Notifications</h3>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-text-muted mb-1">Bot Token</label>
            <input
              type="password"
              value={settings.telegramToken}
              onChange={(e) => setSettings({ ...settings, telegramToken: e.target.value })}
              className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                         focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50"
            />
          </div>
          <div>
            <label className="block text-xs text-text-muted mb-1">Chat ID</label>
            <input
              type="text"
              value={settings.telegramChatId}
              onChange={(e) => setSettings({ ...settings, telegramChatId: e.target.value })}
              className="w-full bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                         focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50"
            />
          </div>
        </div>
      </div>

      <div className="card">
        <h3 className="font-semibold text-sm mb-4">Farm Settings</h3>
        <div className="space-y-3">
          <div>
            <label className="block text-xs text-text-muted mb-1">Max Sandboxes</label>
            <input
              type="number"
              value={settings.maxSandboxes}
              onChange={(e) => setSettings({ ...settings, maxSandboxes: parseInt(e.target.value) || 10 })}
              className="w-32 bg-bg-primary border border-border-default rounded-lg px-3 py-2 text-sm
                         focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/50"
            />
          </div>
          {[
            { key: 'autoCollect', label: 'Auto-collect drops when available' },
            { key: 'autoLoot', label: 'Auto-send drops to trade link' },
            { key: 'replaceOnError', label: 'Replace accounts on error' },
          ].map((toggle) => (
            <label key={toggle.key} className="flex items-center gap-3 cursor-pointer">
              <div
                onClick={() => setSettings({ ...settings, [toggle.key]: !(settings as Record<string, unknown>)[toggle.key] })}
                className={`w-10 h-5 rounded-full transition-colors cursor-pointer relative ${
                  (settings as Record<string, unknown>)[toggle.key] ? 'bg-accent' : 'bg-bg-tertiary'
                }`}
              >
                <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform ${
                  (settings as Record<string, unknown>)[toggle.key] ? 'translate-x-5' : 'translate-x-0.5'
                }`} />
              </div>
              <span className="text-sm">{toggle.label}</span>
            </label>
          ))}
        </div>
      </div>

      <button className="btn-primary flex items-center gap-2">
        <Save className="w-4 h-4" /> Save Settings
      </button>
    </div>
  );
}
