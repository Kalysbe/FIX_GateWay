-- ARQA Position Report stores PosKeys/PosItems and futures kinds (FL/FP).
ALTER TABLE fix_positions DROP CONSTRAINT IF EXISTS fix_positions_kind_check;
ALTER TABLE fix_positions
  ADD CONSTRAINT fix_positions_kind_check
  CHECK (kind IN ('INSTRUMENT', 'CASH', 'FUTURES'));

ALTER TABLE fix_positions
  ADD COLUMN IF NOT EXISTS update_action TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS keys JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS items JSONB NOT NULL DEFAULT '{}'::jsonb;
