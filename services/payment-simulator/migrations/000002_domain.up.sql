CREATE TABLE IF NOT EXISTS payment_attempts (
    id              TEXT PRIMARY KEY,
    operation_id    TEXT NOT NULL UNIQUE,
    order_id        TEXT NOT NULL,
    amount_cents    BIGINT NOT NULL,
    currency        TEXT NOT NULL DEFAULT 'USD',
    scenario        TEXT NOT NULL DEFAULT 'success',
    status          TEXT NOT NULL CHECK (status IN ('CREATED', 'PENDING', 'SUCCEEDED', 'FAILED', 'UNKNOWN')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS compensations (
    id              TEXT PRIMARY KEY,
    operation_id    TEXT NOT NULL UNIQUE,
    payment_id      TEXT NOT NULL REFERENCES payment_attempts(id),
    order_id        TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('CREATED', 'PENDING', 'SUCCEEDED', 'FAILED', 'UNKNOWN')),
    attempt_count   INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
