ALTER TABLE payment_attempts
    ADD COLUMN IF NOT EXISTS resolve_after TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS callback_count INT NOT NULL DEFAULT 0;

ALTER TABLE compensations
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS escalated BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS payment_attempts_pending_resolve_idx
    ON payment_attempts (status, resolve_after)
    WHERE status IN ('PENDING', 'UNKNOWN');
