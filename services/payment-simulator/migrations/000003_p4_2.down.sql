DROP INDEX IF EXISTS payment_attempts_pending_resolve_idx;
ALTER TABLE compensations DROP COLUMN IF EXISTS last_attempt_at;
ALTER TABLE compensations DROP COLUMN IF EXISTS escalated;
ALTER TABLE payment_attempts DROP COLUMN IF EXISTS callback_count;
ALTER TABLE payment_attempts DROP COLUMN IF EXISTS resolve_after;
