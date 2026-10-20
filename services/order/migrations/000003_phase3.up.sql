-- Phase 3: serialize allowance per buyer+session; durable request hash for idempotency.

CREATE TABLE IF NOT EXISTS allowance_scopes (
    buyer_id    TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (buyer_id, session_id)
);

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS request_hash TEXT NOT NULL DEFAULT '';

-- Backfill is fine for empty/dev DBs; new inserts always set hash.
CREATE INDEX IF NOT EXISTS orders_request_hash_idx ON orders (buyer_id, idempotency_key);
