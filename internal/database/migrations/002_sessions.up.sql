CREATE TABLE account_groups (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    game_type   TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE farm_sessions (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT,
    game_type     TEXT NOT NULL,
    farm_mode     TEXT NOT NULL DEFAULT 'sandbox',
    account_ids   BIGINT[] NOT NULL,
    started_at    TIMESTAMPTZ DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    total_hours   REAL DEFAULT 0,
    drops_count   INT DEFAULT 0,
    status        TEXT DEFAULT 'active',
    config        JSONB DEFAULT '{}'
);

CREATE INDEX idx_sessions_status ON farm_sessions(status);
CREATE INDEX idx_sessions_game ON farm_sessions(game_type);
