import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          primary: '#0a0e1a',
          secondary: '#111827',
          tertiary: '#1e2538',
          elevated: '#1a1f35',
        },
        border: {
          default: '#1e293b',
          hover: '#334155',
          accent: '#3B82F6',
        },
        text: {
          primary: '#f1f5f9',
          secondary: '#94a3b8',
          muted: '#475569',
        },
        accent: {
          DEFAULT: '#3B82F6',
          hover: '#2563EB',
          glow: 'rgba(59,130,246,0.15)',
        },
        status: {
          idle: '#6b7280',
          ready: '#e2e8f0',
          active: '#22c55e',
          queued: '#eab308',
          farmed: '#a855f7',
          collected: '#f97316',
          done: '#06b6d4',
          error: '#ef4444',
        },
        game: {
          cs2: '#de9b35',
          dota2: '#c23c2a',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'monospace'],
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'glow': 'glow 2s ease-in-out infinite alternate',
        'fade-in': 'fadeIn 0.2s ease-out',
        'slide-in': 'slideIn 0.3s ease-out',
      },
      keyframes: {
        glow: {
          '0%': { boxShadow: '0 0 5px rgba(59,130,246,0.1)' },
          '100%': { boxShadow: '0 0 20px rgba(59,130,246,0.3)' },
        },
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideIn: {
          '0%': { transform: 'translateX(-10px)', opacity: '0' },
          '100%': { transform: 'translateX(0)', opacity: '1' },
        },
      },
    },
  },
  plugins: [],
} satisfies Config;
