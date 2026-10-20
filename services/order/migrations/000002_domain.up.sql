CREATE TABLE IF NOT EXISTS orders (
    id                      TEXT PRIMARY KEY,
    buyer_id                TEXT NOT NULL,
    session_id              TEXT NOT NULL,
    ticket_type_id          TEXT NOT NULL,
    pool_id                 TEXT NOT NULL,
    quantity                INT NOT NULL CHECK (quantity > 0),
    status                  TEXT NOT NULL,
    entitlement_id          TEXT NOT NULL,
    checkout_operation_id   TEXT NOT NULL,
    inventory_operation_id  TEXT NOT NULL,
    payment_operation_id    TEXT NOT NULL,
    reservation_id          TEXT,
    payment_id              TEXT,
    amount_cents            BIGINT NOT NULL DEFAULT 0,
    currency                TEXT NOT NULL DEFAULT 'USD',
    policy_version          INT NOT NULL DEFAULT 1,
    max_per_buyer           INT NOT NULL DEFAULT 4,
    idempotency_key         TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (buyer_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS allowance_holds (
    id              TEXT PRIMARY KEY,
    buyer_id        TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    order_id        TEXT NOT NULL REFERENCES orders(id),
    quantity        INT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('HELD', 'COMMITTED', 'RELEASED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS allowance_buyer_session_idx ON allowance_holds(buyer_id, session_id);

CREATE TABLE IF NOT EXISTS outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_id        TEXT NOT NULL UNIQUE,
    event_type      TEXT NOT NULL,
    schema_version  INT NOT NULL DEFAULT 1,
    aggregate_id    TEXT NOT NULL,
    payload         JSONB NOT NULL,
    correlation_id  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    principal_id     TEXT NOT NULL,
    operation_type   TEXT NOT NULL,
    resource_context TEXT NOT NULL,
    idempotency_key  TEXT NOT NULL,
    request_hash     TEXT NOT NULL,
    order_id         TEXT,
    state            TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, operation_type, resource_context, idempotency_key)
);
