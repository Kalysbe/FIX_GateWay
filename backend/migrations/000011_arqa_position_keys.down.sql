ALTER TABLE fix_positions
  DROP COLUMN IF EXISTS update_action,
  DROP COLUMN IF EXISTS keys,
  DROP COLUMN IF EXISTS items;

ALTER TABLE fix_positions DROP CONSTRAINT IF EXISTS fix_positions_kind_check;
ALTER TABLE fix_positions
  ADD CONSTRAINT fix_positions_kind_check
  CHECK (kind IN ('INSTRUMENT', 'CASH'));
