import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

// Должен совпадать с cmd/server (HTTP_PORT, по умолчанию 8080). Иначе Vite шлёт /api на не тот порт → 404.
const apiOrigin = process.env.VITE_API_ORIGIN ?? 'http://127.0.0.1:8080';
const wsTarget = (() => {
  const u = new URL(apiOrigin);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.toString();
})();

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    target: 'esnext',
  },
  // @novnc/novnc 1.6+ использует top-level await в CJS — esbuild при pre-bundle падает на require(...).
  optimizeDeps: {
    exclude: ['@novnc/novnc'],
    esbuildOptions: {
      target: 'esnext',
    },
  },
  server: {
    // true = 0.0.0.0 — панель доступна по IP машины (LAN / внешняя сеть), не только localhost
    host: true,
    port: 3001,
    strictPort: true,
    // при доступе по Tailscale/LAN задайте: VITE_DEV_ORIGIN=http://100.x.x.x:3001
    origin: process.env.VITE_DEV_ORIGIN || undefined,
    // иначе Vite может отклонять запросы по IP/домену
    allowedHosts: true,
    proxy: {
      '/api': apiOrigin,
      '/ws': {
        target: wsTarget,
        ws: true,
      },
    },
  },
  preview: {
    host: true,
    port: 4173,
    strictPort: true,
  },
});
