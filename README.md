# Steam Farm System

CS2 Case + Dota 2 Event farming system with a hybrid approach: Steam protocol emulation for lightweight tasks, Docker sandboxes for XP farming through actual game launches.

## Architecture

- **Main Server** (Go + React) -- REST API, WebSocket real-time updates, worker management, PostgreSQL database
- **Desktop Client** (Wails: Go + React) -- runs on each farm machine, manages bots and Docker sandboxes
- **Web Panel** -- dark gaming UI with blue accent, FSM-style farm tables, drop/reward management

## Quick Start

```bash
# Start PostgreSQL
docker-compose up -d postgres

# Run the server (auto-migrates DB)
make dev

# In another terminal -- start the web panel dev server
cd web && npm run dev
```

The web panel is available at `http://localhost:3000`, API at `http://localhost:8080`.

## Project Structure

```
cmd/server/          Server entry point
internal/
  server/            REST API, WebSocket, gRPC, Telegram bot
  engine/            Farm engine (Steam client, CS2/Dota2 GC, sandbox manager)
  database/          PostgreSQL, migrations, models, repositories
  proto/             Protocol definitions
  common/            Shared utilities (config, crypto)
web/                 React web panel (Vite + Tailwind)
docker/              Dockerfiles and configs for game sandboxes
```

## Features

### CS2 Farm (FSM Panel-style)
- Account status color-coding (idle, ready, farming, farmed, collected, error)
- XP progress bars, level tracking, Prime status
- Weekly drop detection and Care Package claim
- Armory Pass star tracking
- Auto-collect, auto-loot (trade link), ban checker

### Dota 2 Farm
- Hour farming with event progress tracking
- Quarter/event reward detection and choice UI
- Pending rewards with pulse notification

### Sandbox Mode (Priority)
- Docker containers per account with HWID isolation
- Minimal game configs: 640x480, 15 FPS, all Low settings
- GPU passthrough (NVIDIA/AMD), Xvfb virtual display
- noVNC monitoring, resource limits (cgroups)

### Protocol Mode (Lightweight)
- Steam CM server connection with TOTP 2FA
- ClientGamesPlayed for playtime, GC message monitoring
- ~5-10 MB RAM per account

## Tech Stack

- **Go**: paralin/go-steam, Gin, pgx/v5, gorilla/websocket
- **React**: Vite, TypeScript, TanStack Query, Tailwind CSS, Recharts
- **PostgreSQL 16**: golang-migrate, pgxpool
- **Docker SDK**: Container management for sandboxes
- **Linux**: Ubuntu 22.04+, Docker 24+, NVIDIA Container Toolkit

## System Requirements

| Mode | CPU | RAM | GPU |
|------|-----|-----|-----|
| Protocol only | 2 cores | 4 GB | None |
| 10 CS2 sandboxes | 6 cores | 32 GB | 4+ GB VRAM |
| 20+ sandboxes | 8+ cores | 64 GB | 8+ GB VRAM |
