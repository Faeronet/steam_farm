CREATE TABLE dota_event_progress (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT REFERENCES accounts(id) ON DELETE CASCADE UNIQUE,
    event_id        TEXT,
    event_name      TEXT,
    current_act     INT DEFAULT 1,
    current_node    INT DEFAULT 0,
    tokens_earned   INT DEFAULT 0,
    tokens_spent    INT DEFAULT 0,
    level           INT DEFAULT 0,
    rewards_pending JSONB DEFAULT '[]',
    rewards_claimed JSONB DEFAULT '[]',
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_events_account ON dota_event_progress(account_id);
