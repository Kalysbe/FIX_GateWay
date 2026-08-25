CREATE TABLE IF NOT EXISTS fix_position_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    adapter_id      TEXT NOT NULL,
    pos_req_id      TEXT NOT NULL,
    clearing_date   TEXT NOT NULL DEFAULT '',
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING', 'OK', 'ERROR', 'TIMEOUT')),
    error           TEXT NOT NULL DEFAULT '',
    report_count    INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_fix_pos_snap_requested ON fix_position_snapshots(requested_at DESC);

CREATE TABLE IF NOT EXISTS fix_positions (
    id              BIGSERIAL PRIMARY KEY,
    snapshot_id     BIGINT NOT NULL REFERENCES fix_position_snapshots(id) ON DELETE CASCADE,
    adapter_id      TEXT NOT NULL,
    pos_maint_rpt_id TEXT NOT NULL DEFAULT '',
    pos_req_id      TEXT NOT NULL DEFAULT '',
    clearing_date   TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL CHECK (kind IN ('INSTRUMENT', 'CASH')),
    firm_id         TEXT NOT NULL DEFAULT '',
    client_code     TEXT NOT NULL DEFAULT '',
    account_code    TEXT NOT NULL DEFAULT '',
    symbol          TEXT NOT NULL DEFAULT '',
    security_id     TEXT NOT NULL DEFAULT '',
    board           TEXT NOT NULL DEFAULT '',
    currency        TEXT NOT NULL DEFAULT '',
    pos_type        TEXT NOT NULL DEFAULT '',
    long_qty        NUMERIC(24, 8) NOT NULL DEFAULT 0,
    short_qty       NUMERIC(24, 8) NOT NULL DEFAULT 0,
    net_qty         NUMERIC(24, 8) NOT NULL DEFAULT 0,
    pos_amt_type    TEXT NOT NULL DEFAULT '',
    pos_amt         NUMERIC(24, 8) NOT NULL DEFAULT 0,
    settl_price     NUMERIC(24, 8) NOT NULL DEFAULT 0,
    raw_fix         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_fix_positions_snapshot ON fix_positions(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_fix_positions_kind ON fix_positions(kind, firm_id, client_code);
CREATE INDEX IF NOT EXISTS idx_fix_positions_lookup ON fix_positions(adapter_id, kind, firm_id);
