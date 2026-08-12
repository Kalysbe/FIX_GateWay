-- Per-order class code (QUIK board) → FIX SecurityExchange tag 207.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS board TEXT NOT NULL DEFAULT '';
