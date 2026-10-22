DROP INDEX idx_executions_trade_match;
ALTER TABLE executions DROP COLUMN trade_match_id, DROP COLUMN contra_broker, DROP COLUMN contra_trader;
