-- Durable admission budgets (PostgreSQL authority for multi-instance correctness).
-- Redis may later mirror these counters; it must not be the sole source of truth.

CREATE TABLE IF NOT EXISTS budget_scopes (
    scope_key            TEXT PRIMARY KEY, -- 'global' or 'session:<id>'
    max_grants           INT NOT NULL CHECK (max_grants >= 0),
    grants_issued        INT NOT NULL DEFAULT 0 CHECK (grants_issued >= 0),
    max_active_checkouts INT NOT NULL CHECK (max_active_checkouts >= 0),
    active_checkouts     INT NOT NULL DEFAULT 0 CHECK (active_checkouts >= 0),
    coordination_epoch   BIGINT NOT NULL DEFAULT 1,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (grants_issued <= max_grants),
    CHECK (active_checkouts <= max_active_checkouts)
);

INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
VALUES ('global', 100000, 10000)
ON CONFLICT (scope_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS processed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
