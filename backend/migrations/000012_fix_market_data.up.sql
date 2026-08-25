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

CREATE INDEX IF NOT EXISTS idx_fix_md_snap_requested ON fix_md_snapshots(requested_at DESC);

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

CREATE INDEX IF NOT EXISTS idx_fix_md_quotes_snapshot ON fix_md_quotes(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_fix_md_quotes_lookup ON fix_md_quotes(board, symbol);
