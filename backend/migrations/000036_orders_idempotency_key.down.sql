DROP INDEX IF EXISTS orders_broker_idempotency_key_uq;
ALTER TABLE orders DROP COLUMN IF EXISTS idempotency_key;
