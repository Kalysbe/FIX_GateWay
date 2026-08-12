-- Broker-supplied trading account code for FIX tag 1 (Account).
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fix_account TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ALTER COLUMN account_id DROP NOT NULL;
