-- P5.4.2: durable W3C trace carrier for async outbox publish (JSON map).
-- Nullable: rows created before this migration publish as new roots without links.
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS trace_carrier TEXT;
