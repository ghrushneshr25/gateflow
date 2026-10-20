ALTER TABLE outbox
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS locked_by TEXT,
    ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error TEXT,
    ADD COLUMN IF NOT EXISTS quarantine BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
    ON outbox (id)
    WHERE published_at IS NULL AND quarantine = false;

-- Consumer logical effect: durable audit of confirmed order events (idempotent via processed_events).
CREATE TABLE IF NOT EXISTS order_confirmed_effects (
    event_id     TEXT PRIMARY KEY,
    order_id     TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
