-- Observability: partitioned event log, notifications, export jobs, order correlation.

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS request_id TEXT,
    ADD COLUMN IF NOT EXISTS trace_id TEXT;

CREATE INDEX IF NOT EXISTS idx_orders_request_id ON orders (request_id) WHERE request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_trace_id ON orders (trace_id) WHERE trace_id IS NOT NULL;

CREATE TABLE obs_events (
    id                UUID        NOT NULL DEFAULT gen_random_uuid(),
    ts                TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type        TEXT        NOT NULL,
    subtype           TEXT        NOT NULL DEFAULT '',
    severity          TEXT        NOT NULL DEFAULT 'INFO',
    request_id        TEXT,
    trace_id          TEXT,
    broker_id         BIGINT,
    broker_code       TEXT,
    broker_name       TEXT,
    token_id          UUID,
    client_ip         TEXT,
    forwarded_for     TEXT,
    user_agent        TEXT,
    http_method       TEXT,
    endpoint          TEXT,
    route             TEXT,
    query_params      TEXT,
    protocol          TEXT,
    status_code       INT,
    response_time_ms  INT,
    request_size      INT,
    response_size     INT,
    perf_class        TEXT,
    result            TEXT,
    order_id          UUID,
    client_order_id   TEXT,
    account_id        TEXT,
    instrument        TEXT,
    side              TEXT,
    quantity          TEXT,
    price             TEXT,
    adapter_id        TEXT,
    session_id        TEXT,
    fix_msg_type      TEXT,
    fix_direction     TEXT,
    fix_seq_num       INT,
    error_code        TEXT,
    error_message     TEXT,
    failure_stage     TEXT,
    message           TEXT,
    actor_type        TEXT,
    actor_id          TEXT,
    target_type       TEXT,
    target_id         TEXT,
    before_state      JSONB,
    after_state       JSONB,
    metadata          JSONB,
    PRIMARY KEY (ts, id)
) PARTITION BY RANGE (ts);

CREATE TABLE obs_events_default PARTITION OF obs_events DEFAULT;

DO $$
DECLARE
    d date := date '2026-06-01';
    part_name text;
BEGIN
    WHILE d < date '2028-01-01' LOOP
        part_name := format('obs_events_%s', to_char(d, 'YYYY_MM'));
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF obs_events FOR VALUES FROM (%L) TO (%L)',
            part_name, d, (d + interval '1 month')::date
        );
        d := (d + interval '1 month')::date;
    END LOOP;
END $$;

CREATE INDEX idx_obs_broker_ts ON obs_events (broker_id, ts DESC);
CREATE INDEX idx_obs_ts ON obs_events (ts DESC);
CREATE INDEX idx_obs_request_id ON obs_events (request_id) WHERE request_id IS NOT NULL;
CREATE INDEX idx_obs_trace_id ON obs_events (trace_id) WHERE trace_id IS NOT NULL;
CREATE INDEX idx_obs_order_id ON obs_events (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX idx_obs_client_order_id ON obs_events (client_order_id) WHERE client_order_id IS NOT NULL;
CREATE INDEX idx_obs_type_ts ON obs_events (event_type, ts DESC);
CREATE INDEX idx_obs_subtype_ts ON obs_events (subtype, ts DESC);
CREATE INDEX idx_obs_severity_ts ON obs_events (severity, ts DESC);
CREATE INDEX idx_obs_status_ts ON obs_events (status_code, ts DESC) WHERE status_code IS NOT NULL;
CREATE INDEX idx_obs_ip_ts ON obs_events (client_ip, ts DESC) WHERE client_ip IS NOT NULL;
CREATE INDEX idx_obs_adapter_ts ON obs_events (adapter_id, ts DESC) WHERE adapter_id IS NOT NULL;
CREATE INDEX idx_obs_type_subtype_ts ON obs_events (event_type, subtype, ts DESC);
CREATE INDEX idx_obs_error_code_ts ON obs_events (error_code, ts DESC) WHERE error_code IS NOT NULL;
CREATE INDEX idx_obs_broker_type_ts ON obs_events (broker_id, event_type, ts DESC);

CREATE TABLE obs_notifications (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fingerprint   TEXT        NOT NULL,
    severity      TEXT        NOT NULL,
    event_type    TEXT        NOT NULL DEFAULT '',
    subtype       TEXT        NOT NULL DEFAULT '',
    error_code    TEXT        NOT NULL DEFAULT '',
    broker_id     BIGINT,
    broker_name   TEXT,
    title         TEXT        NOT NULL,
    message       TEXT        NOT NULL DEFAULT '',
    count         INT         NOT NULL DEFAULT 1,
    window_start  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at       TIMESTAMPTZ,
    silenced_at   TIMESTAMPTZ,
    metadata      JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_obs_notif_unread ON obs_notifications (last_seen_at DESC) WHERE read_at IS NULL;
CREATE INDEX idx_obs_notif_fp_window ON obs_notifications (fingerprint, window_start DESC);
CREATE INDEX idx_obs_notif_broker ON obs_notifications (broker_id, last_seen_at DESC);

CREATE TABLE obs_export_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_user_id TEXT,
    format        TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'PENDING',
    filter        JSONB,
    file_path     TEXT,
    row_count     INT,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ
);

CREATE INDEX idx_obs_export_created ON obs_export_jobs (created_at DESC);
