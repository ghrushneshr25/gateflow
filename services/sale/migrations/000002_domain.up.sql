CREATE TABLE IF NOT EXISTS offerings (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT 'general',
    status        TEXT NOT NULL CHECK (status IN ('DRAFT', 'PUBLISHED', 'UNPUBLISHED')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    offering_id     TEXT NOT NULL REFERENCES offerings(id),
    starts_at       TIMESTAMPTZ NOT NULL,
    ends_at         TIMESTAMPTZ NOT NULL,
    sale_opens_at   TIMESTAMPTZ NOT NULL,
    sale_closes_at  TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('SCHEDULED', 'ON_SALE', 'SALE_CLOSED', 'COMPLETED', 'CANCELLED')),
    admission_strategy TEXT NOT NULL DEFAULT 'fifo',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    CHECK (sale_closes_at > sale_opens_at)
);

CREATE INDEX IF NOT EXISTS sessions_offering_idx ON sessions(offering_id);

CREATE TABLE IF NOT EXISTS ticket_types (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    name            TEXT NOT NULL,
    section         TEXT NOT NULL DEFAULT 'GA',
    capacity        INT NOT NULL CHECK (capacity > 0),
    price_cents     BIGINT NOT NULL CHECK (price_cents >= 0),
    currency        TEXT NOT NULL DEFAULT 'USD',
    inventory_pool_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ticket_types_session_idx ON ticket_types(session_id);

CREATE TABLE IF NOT EXISTS purchase_policies (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    version         INT NOT NULL,
    max_per_buyer   INT NOT NULL CHECK (max_per_buyer > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, version)
);
