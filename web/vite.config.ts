import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

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
    port: 3001,
    proxy: {
      '/api': 'http://localhost:8085',
      '/ws': {
        target: 'ws://localhost:8085',
        ws: true,
      },
    },
  },
});
