ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS fix_send_attempted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_outbox_stale_processing
    ON outbox_events (locked_at)
    WHERE status = 'PROCESSING';
