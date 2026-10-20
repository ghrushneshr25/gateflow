ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS payment_deadline_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS compensation_operation_id TEXT;
