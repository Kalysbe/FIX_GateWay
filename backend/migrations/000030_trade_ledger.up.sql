BEGIN;
CREATE TABLE trades (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trade_match_id TEXT NOT NULL,
    quik_trade_num TEXT NOT NULL CHECK (quik_trade_num ~ '^[0-9]+$'),
    trade_date DATE NOT NULL,
    symbol TEXT NOT NULL,
    board TEXT NOT NULL,
    quantity NUMERIC NOT NULL CHECK (quantity > 0),
    price NUMERIC NOT NULL CHECK (price >= 0),
    traded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (trade_match_id, trade_date, symbol, board)
);
CREATE INDEX trades_recent_idx ON trades(trade_date DESC, length(quik_trade_num) DESC, quik_trade_num DESC);
CREATE TABLE trade_reports (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source TEXT NOT NULL CHECK (source IN ('fix','quik')),
    source_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    adapter_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'RECEIVED',
    error TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, source_key)
);
CREATE TABLE trade_sides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trade_id UUID NOT NULL REFERENCES trades(id),
    side TEXT NOT NULL CHECK (side IN ('BUY','SELL')),
    quik_order_id TEXT NOT NULL,
    quik_order_num TEXT NOT NULL,
    order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
    execution_id UUID REFERENCES executions(id) ON DELETE SET NULL,
    exec_id TEXT NOT NULL DEFAULT '',
    fix_cl_ord_id TEXT NOT NULL DEFAULT '',
    firm_id TEXT NOT NULL DEFAULT '',
    firm_name TEXT NOT NULL DEFAULT '',
    client_code TEXT NOT NULL DEFAULT '',
    account TEXT NOT NULL DEFAULT '',
    contra_broker TEXT NOT NULL DEFAULT '',
    cum_qty NUMERIC,
    leaves_qty NUMERIC,
    adapter_id TEXT NOT NULL DEFAULT '',
    has_fix_report BOOLEAN NOT NULL DEFAULT false,
    has_quik_record BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(trade_id,side)
);
CREATE INDEX trade_sides_order_idx ON trade_sides(order_id) WHERE order_id IS NOT NULL;
CREATE INDEX trade_sides_execution_idx ON trade_sides(execution_id) WHERE execution_id IS NOT NULL;
CREATE TABLE trade_side_reports (
    report_id BIGINT PRIMARY KEY REFERENCES trade_reports(id),
    side_id UUID NOT NULL REFERENCES trade_sides(id)
);
CREATE TABLE trade_sync_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    source_url TEXT NOT NULL,
    records INTEGER NOT NULL DEFAULT 0,
    errors INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);
COMMENT ON TABLE trades IS 'Exchange trades, identified by QUIK trade number, date, instrument and class.';
COMMENT ON TABLE trade_sides IS 'Only actual sides observed in FIX reports or QUIK history. No inferred counterpart rows.';
COMMENT ON TABLE trade_reports IS 'Persistent source evidence, independent of local Gateway orders.';
COMMIT;
