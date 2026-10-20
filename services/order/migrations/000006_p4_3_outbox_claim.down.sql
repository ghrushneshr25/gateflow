DROP TABLE IF EXISTS order_confirmed_effects;
DROP INDEX IF EXISTS outbox_unpublished_idx;
ALTER TABLE outbox DROP COLUMN IF EXISTS quarantine;
ALTER TABLE outbox DROP COLUMN IF EXISTS last_error;
ALTER TABLE outbox DROP COLUMN IF EXISTS attempt_count;
ALTER TABLE outbox DROP COLUMN IF EXISTS locked_by;
ALTER TABLE outbox DROP COLUMN IF EXISTS locked_until;
