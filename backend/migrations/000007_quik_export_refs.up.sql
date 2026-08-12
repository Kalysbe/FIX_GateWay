-- QUIK export reference: firms (read-synced from remote "Firms").
CREATE TABLE IF NOT EXISTS quik_firms (
    firm_id     TEXT PRIMARY KEY,
    exchange    TEXT NOT NULL DEFAULT '',
    firm_name   TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    status_flag INT,
    synced_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Same SecCode can exist on multiple ClassCodes; upsert by (quik_symbol, board).
CREATE UNIQUE INDEX IF NOT EXISTS uq_instruments_quik_board
    ON instruments (quik_symbol, board);
