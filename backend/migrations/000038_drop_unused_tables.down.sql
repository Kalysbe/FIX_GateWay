-- Recreates the empty tables; dropped rows are not restored.
CREATE TABLE broker_accounts (
    id              BIGSERIAL PRIMARY KEY,
    broker_id       BIGINT NOT NULL REFERENCES brokers(id),
    code            TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    quik_firm       TEXT NOT NULL DEFAULT '',
    trading_account TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (broker_id, code)
);
CREATE TABLE broker_instrument_mapping (
    id               BIGSERIAL PRIMARY KEY,
    broker_id        BIGINT NOT NULL REFERENCES brokers(id),
    broker_symbol    TEXT NOT NULL,
    instrument_id    BIGINT NOT NULL REFERENCES instruments(id),
    UNIQUE (broker_id, broker_symbol)
);
CREATE TABLE api_logs (
    id            BIGSERIAL PRIMARY KEY,
    method        TEXT NOT NULL,
    path          TEXT NOT NULL,
    status_code   INT NOT NULL,
    broker_id     BIGINT,
    user_id       UUID,
    ip            TEXT,
    duration_ms   INT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS position_overrides (
    id              BIGSERIAL PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('CASH', 'INSTRUMENT')),
    firm_id         TEXT NOT NULL,
    client_code     TEXT NOT NULL,
    account_code    TEXT NOT NULL DEFAULT '',
    symbol          TEXT NOT NULL DEFAULT '',
    security_id     TEXT NOT NULL DEFAULT '',
    board           TEXT NOT NULL DEFAULT '',
    currency        TEXT NOT NULL DEFAULT '',
    long_qty        NUMERIC(24, 8) NOT NULL DEFAULT 0,
    short_qty       NUMERIC(24, 8) NOT NULL DEFAULT 0,
    net_qty         NUMERIC(24, 8) NOT NULL DEFAULT 0,
    pos_amt         NUMERIC(24, 8) NOT NULL DEFAULT 0,
    settl_price     NUMERIC(24, 8) NOT NULL DEFAULT 0,
    keys            JSONB NOT NULL DEFAULT '{}'::jsonb,
    items           JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_position_overrides_uniq
    ON position_overrides (
        kind,
        (lower(trim(firm_id))),
        (lower(trim(client_code))),
        (lower(trim(symbol))),
        (lower(trim(board))),
        (lower(trim(currency)))
    );
CREATE INDEX IF NOT EXISTS idx_position_overrides_firm
    ON position_overrides (kind, firm_id, client_code);
CREATE TABLE IF NOT EXISTS fix_md_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    adapter_id      TEXT NOT NULL,
    md_req_id       TEXT NOT NULL,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING', 'OK', 'ERROR', 'TIMEOUT')),
    error           TEXT NOT NULL DEFAULT '',
    report_count    INT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS fix_md_quotes (
    id                    BIGSERIAL PRIMARY KEY,
    snapshot_id           BIGINT NOT NULL REFERENCES fix_md_snapshots(id) ON DELETE CASCADE,
    adapter_id            TEXT NOT NULL DEFAULT '',
    md_req_id             TEXT NOT NULL DEFAULT '',
    symbol                TEXT NOT NULL DEFAULT '',
    security_id           TEXT NOT NULL DEFAULT '',
    board                 TEXT NOT NULL DEFAULT '',
    currency              TEXT NOT NULL DEFAULT '',
    name                  TEXT NOT NULL DEFAULT '',
    trade_date            TEXT NOT NULL DEFAULT '',
    trading_status        TEXT NOT NULL DEFAULT '',
    trading_session_id    TEXT NOT NULL DEFAULT '',
    trading_session_sub   TEXT NOT NULL DEFAULT '',
    bid                   TEXT NOT NULL DEFAULT '',
    bid_size              TEXT NOT NULL DEFAULT '',
    offer                 TEXT NOT NULL DEFAULT '',
    offer_size            TEXT NOT NULL DEFAULT '',
    last                  TEXT NOT NULL DEFAULT '',
    last_size             TEXT NOT NULL DEFAULT '',
    last_time             TEXT NOT NULL DEFAULT '',
    open                  TEXT NOT NULL DEFAULT '',
    high                  TEXT NOT NULL DEFAULT '',
    low                   TEXT NOT NULL DEFAULT '',
    close                 TEXT NOT NULL DEFAULT '',
    wapr                  TEXT NOT NULL DEFAULT '',
    settle                TEXT NOT NULL DEFAULT '',
    current_price         TEXT NOT NULL DEFAULT '',
    official_price        TEXT NOT NULL DEFAULT '',
    volume                TEXT NOT NULL DEFAULT '',
    value                 TEXT NOT NULL DEFAULT '',
    entries               JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_fix               TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fix_md_snap_requested ON fix_md_snapshots(requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_fix_md_quotes_snapshot ON fix_md_quotes(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_fix_md_quotes_lookup ON fix_md_quotes(board, symbol);
UPDATE orders SET account_id = NULL WHERE account_id IS NOT NULL;
ALTER TABLE orders ADD CONSTRAINT orders_account_id_fkey FOREIGN KEY (account_id) REFERENCES broker_accounts(id);
CREATE OR REPLACE FUNCTION gateway_dashboard_compute() RETURNS jsonb
LANGUAGE sql STABLE AS $$
WITH day_events AS MATERIALIZED (
 SELECT ts,event_type,subtype,result,response_time_ms,severity,broker_id,broker_name,error_code
 FROM obs_events WHERE ts >= date_trunc('day', now())
), overview AS (
 SELECT count(*) FILTER (WHERE event_type='API_REQUEST') AS requests_today,
 count(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE') AS orders_today,
 count(*) FILTER (WHERE event_type='API_REQUEST' AND result='SUCCESS') AS successful,
 count(*) FILTER (WHERE event_type='API_REQUEST' AND result='FAILURE') AS failed,
 coalesce(avg(response_time_ms) FILTER (WHERE event_type='API_REQUEST'),0) AS avg_latency_ms,
 coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST'),0) AS p95_latency_ms,
 coalesce(percentile_cont(0.99) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST'),0) AS p99_latency_ms,
 count(*) FILTER (WHERE event_type='ERROR') AS errors_today,
 count(*) FILTER (WHERE severity='CRITICAL') AS critical_today,
 count(*) FILTER (WHERE event_type='AUTH' AND subtype <> 'AUTH_SUCCESS') AS auth_failures_today,
 count(DISTINCT broker_id) AS active_brokers FROM day_events
), order_stats AS (
 SELECT count(*) AS total, count(*) FILTER (WHERE status='FILLED') AS filled,
 count(*) FILTER (WHERE status IN ('REJECTED','FAILED')) AS rejected,
 count(*) FILTER (WHERE status IN ('QUEUED','ROUTING','SENT','ACKNOWLEDGED','PARTIALLY_FILLED','CANCEL_PENDING')) AS active
 FROM orders WHERE created_at >= date_trunc('day',now())
), connected AS (
 SELECT e.broker_id, coalesce(max(b.code),max(e.broker_code),'') AS broker_code,
 coalesce(max(b.name),max(e.broker_name),e.broker_id::text) AS broker_name,
 max(e.ts) AS last_activity,
 (array_agg(e.client_ip ORDER BY e.ts DESC) FILTER (WHERE e.client_ip IS NOT NULL AND e.client_ip<>''))[1] AS last_ip,
 count(*) AS requests FROM obs_events e LEFT JOIN brokers b ON b.id=e.broker_id
 WHERE e.ts >= now()-interval '2 minutes' AND e.broker_id IS NOT NULL AND e.event_type='API_REQUEST'
 GROUP BY e.broker_id
), broker_counts AS (
 SELECT broker_id, coalesce(max(broker_name),'') AS broker_name,
 count(*) FILTER (WHERE event_type='API_REQUEST') AS requests,
 count(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE') AS orders,
 count(*) FILTER (WHERE event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE')) AS errors,
 count(*) FILTER (WHERE event_type='API_REQUEST' AND result='SUCCESS') AS successful,
 coalesce(avg(response_time_ms) FILTER (WHERE event_type='API_REQUEST'),0) AS avg_latency_ms,
 max(ts) AS last_activity FROM day_events WHERE broker_id IS NOT NULL GROUP BY broker_id
), top_brokers AS (
 SELECT broker_id,broker_name,requests,orders,errors,avg_latency_ms,last_activity,
 CASE WHEN requests=0 THEN 100 ELSE successful*100.0/requests END AS success_rate
 FROM broker_counts ORDER BY requests DESC,broker_id LIMIT 10
), top_errors AS (
 SELECT coalesce(nullif(error_code,''),subtype) AS error,count(*) AS count,max(ts) AS last_occurrence,
 count(DISTINCT broker_id) AS affected_brokers FROM day_events
 WHERE (event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE') OR (event_type='AUTH' AND subtype<>'AUTH_SUCCESS'))
 AND coalesce(nullif(error_code,''),subtype)<>'' GROUP BY 1 ORDER BY count(*) DESC,1 LIMIT 10
)
SELECT jsonb_build_object(
 'stats',(SELECT to_jsonb(s) FROM order_stats s),
 'observability',(SELECT to_jsonb(o)||jsonb_build_object('success_rate',CASE WHEN requests_today=0 THEN 100 ELSE successful*100.0/requests_today END,'connected_now',(SELECT count(*) FROM connected)) FROM overview o),
 'brokers',(SELECT count(*) FROM brokers),
 'active_accounts',(SELECT count(*) FROM broker_accounts WHERE status='ACTIVE'),
 'connected_brokers',coalesce((SELECT jsonb_agg(to_jsonb(c) ORDER BY last_activity DESC,broker_id) FROM connected c),'[]'::jsonb),
 'top_brokers',coalesce((SELECT jsonb_agg(to_jsonb(b) ORDER BY requests DESC,broker_id) FROM top_brokers b),'[]'::jsonb),
 'top_errors',coalesce((SELECT jsonb_agg(to_jsonb(e) ORDER BY count DESC,error) FROM top_errors e),'[]'::jsonb),
 'generated_at',now()
);
$$;
UPDATE gateway_dashboard_cache SET refreshed_at = refreshed_at - interval '10 minutes';
