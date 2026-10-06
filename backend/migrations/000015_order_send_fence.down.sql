DROP INDEX IF EXISTS idx_outbox_stale_processing;
ALTER TABLE orders DROP COLUMN IF EXISTS fix_send_attempted_at;
