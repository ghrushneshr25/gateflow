DROP INDEX IF EXISTS orders_request_hash_idx;
ALTER TABLE orders DROP COLUMN IF EXISTS request_hash;
DROP TABLE IF EXISTS allowance_scopes;
