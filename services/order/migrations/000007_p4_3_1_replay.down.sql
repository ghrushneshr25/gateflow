ALTER TABLE outbox DROP COLUMN IF EXISTS last_replay_by;
ALTER TABLE outbox DROP COLUMN IF EXISTS last_replay_at;
ALTER TABLE outbox DROP COLUMN IF EXISTS replay_count;