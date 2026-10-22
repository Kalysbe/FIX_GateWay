ALTER TABLE executions
    ADD COLUMN trade_match_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN contra_broker TEXT NOT NULL DEFAULT '',
    ADD COLUMN contra_trader TEXT NOT NULL DEFAULT '';

-- The execution and its event were committed in the same transaction (same
-- created_at). Restore only metadata unambiguously present in that exact report.
-- Do not create executions, alter quantities, or guess counterpart order IDs.
WITH source AS (
    SELECT e.id,
           min(substring(ev.payload->>'raw_fix' from '[|]880=([^|]+)')) AS match_id,
           min(substring(ev.payload->>'raw_fix' from '[|]375=([^|]+)')) AS broker,
           min(substring(ev.payload->>'raw_fix' from '[|]337=([^|]+)')) AS trader
    FROM executions e
    JOIN order_events ev ON ev.order_id=e.order_id AND ev.created_at=e.created_at
        AND ev.event_type='ORDER_EXECUTION_REPORT'
        AND substring(ev.payload->>'raw_fix' from '[|]17=([^|]+)')=e.exec_id
        AND regexp_count(ev.payload->>'raw_fix','[|]880=')<=1
        AND regexp_count(ev.payload->>'raw_fix','[|]375=')<=1
        AND regexp_count(ev.payload->>'raw_fix','[|]337=')<=1
    GROUP BY e.id
    HAVING count(DISTINCT substring(ev.payload->>'raw_fix' from '[|]880=([^|]+)')) <= 1
       AND count(DISTINCT substring(ev.payload->>'raw_fix' from '[|]375=([^|]+)')) <= 1
       AND count(DISTINCT substring(ev.payload->>'raw_fix' from '[|]337=([^|]+)')) <= 1
)
UPDATE executions e SET trade_match_id=COALESCE(s.match_id,''),
    contra_broker=COALESCE(s.broker,''), contra_trader=COALESCE(s.trader,'')
FROM source s WHERE s.id=e.id;

CREATE INDEX idx_executions_trade_match ON executions(trade_match_id,execution_date)
    WHERE trade_match_id<>'';
