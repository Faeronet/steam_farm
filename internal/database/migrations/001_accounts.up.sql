CREATE TABLE accounts (
    id              BIGSERIAL PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    password_enc    BYTEA NOT NULL,
    shared_secret   TEXT,
    identity_secret TEXT,
    steam_id        BIGINT,
    avatar_url      TEXT,
    persona_name    TEXT,
    proxy           TEXT,
    game_type       TEXT NOT NULL CHECK (game_type IN ('cs2', 'dota2')),
    farm_mode       TEXT NOT NULL DEFAULT 'sandbox' CHECK (farm_mode IN ('protocol', 'sandbox')),
    status          TEXT NOT NULL DEFAULT 'idle',
    status_detail   TEXT,
    is_prime        BOOLEAN DEFAULT FALSE,
    cs2_level       INT DEFAULT 0,
    cs2_xp          INT DEFAULT 0,
    cs2_xp_needed   INT DEFAULT 0,
    cs2_rank        TEXT,
    armory_stars    INT DEFAULT 0,
    dota_hours      REAL DEFAULT 0,
    last_drop_at    TIMESTAMPTZ,
    farmed_this_week BOOLEAN DEFAULT FALSE,
    drop_collected   BOOLEAN DEFAULT FALSE,
    tags            TEXT[] DEFAULT '{}',
    group_name      TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_accounts_game_type ON accounts(game_type);
CREATE INDEX idx_accounts_status ON accounts(status);
CREATE INDEX idx_accounts_group ON accounts(group_name);
