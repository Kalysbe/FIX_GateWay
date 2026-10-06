-- Manual cash/instrument positions set via API. Overlay the latest FIX snapshot
-- on read so background sync does not wipe operator or broker adjustments.
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
