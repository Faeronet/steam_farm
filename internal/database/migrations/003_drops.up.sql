CREATE TABLE drops (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT REFERENCES accounts(id) ON DELETE CASCADE,
    session_id      BIGINT REFERENCES farm_sessions(id),
    game_type       TEXT NOT NULL,
    item_name       TEXT NOT NULL,
    item_type       TEXT,
    item_image_url  TEXT,
    asset_id        BIGINT,
    class_id        BIGINT,
    instance_id     BIGINT,
    context_id      BIGINT DEFAULT 2,
    market_price    REAL,
    dropped_at      TIMESTAMPTZ DEFAULT NOW(),
    claimed         BOOLEAN DEFAULT FALSE,
    sent_to_trade   BOOLEAN DEFAULT FALSE,
    choice_options  JSONB,
    chosen_items    JSONB
);

CREATE INDEX idx_drops_account ON drops(account_id);
CREATE INDEX idx_drops_game ON drops(game_type);
CREATE INDEX idx_drops_claimed ON drops(claimed);
CREATE INDEX idx_drops_date ON drops(dropped_at);
