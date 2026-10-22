-- Complete recent admin trade pairs without scanning the execution history.
CREATE INDEX idx_executions_exec_date ON executions(exec_id, execution_date);
CREATE INDEX idx_executions_created ON executions(created_at DESC, id DESC);
