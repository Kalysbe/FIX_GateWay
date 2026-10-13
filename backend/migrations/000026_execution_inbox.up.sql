BEGIN;
CREATE TABLE execution_inbox_receipts (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX execution_inbox_receipts_order_idx ON execution_inbox_receipts(order_id);

-- A venue execution ID is scoped to the logical order and execution date,
-- independent of which physical adapter delivered a replay.
ALTER TABLE executions DROP CONSTRAINT executions_exec_id_key;
ALTER TABLE executions ADD COLUMN execution_date DATE;
ALTER TABLE executions ADD CONSTRAINT executions_order_date_exec_key
    UNIQUE NULLS NOT DISTINCT (order_id, execution_date, exec_id);
COMMENT ON COLUMN executions.execution_date IS
    'FIX TradeDate or original event UTC date; NULL marks legacy/undated executions.';

COMMIT;
