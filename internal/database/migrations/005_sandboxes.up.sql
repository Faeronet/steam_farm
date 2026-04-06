CREATE TABLE sandboxes (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT REFERENCES accounts(id) ON DELETE CASCADE,
    container_id    TEXT,
    container_name  TEXT,
    game_type       TEXT NOT NULL,
    machine_id      TEXT,
    mac_address     TEXT,
    hostname        TEXT,
    display         TEXT,
    vnc_port        INT,
    status          TEXT DEFAULT 'stopped',
    cpu_usage       REAL DEFAULT 0,
    memory_mb       INT DEFAULT 0,
    gpu_device      TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE proxies (
    id          BIGSERIAL PRIMARY KEY,
    address     TEXT NOT NULL,
    is_alive    BOOLEAN DEFAULT TRUE,
    last_check  TIMESTAMPTZ,
    assigned_to BIGINT REFERENCES accounts(id),
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE weekly_stats (
    id              BIGSERIAL PRIMARY KEY,
    week_start      DATE NOT NULL,
    game_type       TEXT NOT NULL,
    accounts_farmed INT DEFAULT 0,
    total_drops     INT DEFAULT 0,
    total_value     REAL DEFAULT 0,
    drop_breakdown  JSONB DEFAULT '{}',
    UNIQUE(week_start, game_type)
);

CREATE INDEX idx_sandboxes_account ON sandboxes(account_id);
CREATE INDEX idx_sandboxes_status ON sandboxes(status);
CREATE INDEX idx_weekly_stats_week ON weekly_stats(week_start);
