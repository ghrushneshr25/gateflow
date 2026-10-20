CREATE TABLE IF NOT EXISTS pools (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    ticket_type_id  TEXT NOT NULL,
    name            TEXT NOT NULL,
    sellable_capacity INT NOT NULL CHECK (sellable_capacity >= 0),
    confirmed_qty   INT NOT NULL DEFAULT 0 CHECK (confirmed_qty >= 0),
    active_reserved_qty INT NOT NULL DEFAULT 0 CHECK (active_reserved_qty >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (confirmed_qty + active_reserved_qty <= sellable_capacity)
);

CREATE UNIQUE INDEX IF NOT EXISTS pools_ticket_type_uidx ON pools(ticket_type_id);

CREATE TABLE IF NOT EXISTS reservations (
    id              TEXT PRIMARY KEY,
    operation_id    TEXT NOT NULL UNIQUE,
    pool_id         TEXT NOT NULL REFERENCES pools(id),
    quantity        INT NOT NULL CHECK (quantity > 0),
    buyer_id        TEXT NOT NULL,
    order_id        TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('ACTIVE', 'CONFIRMED', 'EXPIRED', 'RELEASED')),
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS reservations_pool_status_idx ON reservations(pool_id, status);
CREATE INDEX IF NOT EXISTS reservations_expires_idx ON reservations(expires_at) WHERE status = 'ACTIVE';
