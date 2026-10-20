CREATE TABLE IF NOT EXISTS queue_entries (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    buyer_id        TEXT NOT NULL,
    sequence_no     BIGSERIAL,
    status          TEXT NOT NULL CHECK (status IN ('JOINED', 'ELIGIBLE', 'GRANTED', 'LEFT', 'EXPIRED', 'REMOVED')),
    entitlement_id  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, buyer_id)
);

CREATE INDEX IF NOT EXISTS queue_entries_session_seq_idx ON queue_entries(session_id, sequence_no);

CREATE TABLE IF NOT EXISTS entitlements (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL,
    buyer_id            TEXT NOT NULL,
    queue_entry_id      TEXT NOT NULL REFERENCES queue_entries(id),
    status              TEXT NOT NULL CHECK (status IN ('ISSUED', 'BOUND', 'CONSUMED', 'EXPIRED', 'REVOKED')),
    unbound_expires_at  TIMESTAMPTZ NOT NULL,
    bound_checkout_op   TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS entitlements_buyer_idx ON entitlements(buyer_id, session_id);
