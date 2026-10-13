BEGIN;
-- Refuse rollback when the old global ExecID constraint cannot represent data.
-- PostgreSQL executes this migration transactionally; no executions are deleted.
ALTER TABLE executions ADD CONSTRAINT executions_exec_id_key UNIQUE(exec_id);
ALTER TABLE executions DROP CONSTRAINT executions_order_date_exec_key;
ALTER TABLE executions DROP COLUMN execution_date;
DROP TABLE execution_inbox_receipts;

COMMIT;
