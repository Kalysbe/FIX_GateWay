-- Восстанавливает только схему: удалённые строки есть в БД QUIK, сюда они не
-- возвращаются. instrument_id у оставшихся заявок восстанавливается по коду.
BEGIN;

CREATE TABLE instruments (
    id BIGSERIAL PRIMARY KEY,
    internal_symbol TEXT NOT NULL UNIQUE,
    quik_symbol TEXT NOT NULL,
    board TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT 'RUB',
    security_type TEXT NOT NULL DEFAULT 'CS',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_instruments_quik_board ON instruments (quik_symbol, board);
INSERT INTO instruments (internal_symbol, quik_symbol, board, currency)
SELECT DISTINCT board || '/' || symbol, symbol, board, currency FROM orders;

ALTER TABLE orders
    ADD COLUMN instrument_id BIGINT REFERENCES instruments(id),
    ADD COLUMN account_id BIGINT,
    ADD COLUMN avg_price NUMERIC(24, 8) NOT NULL DEFAULT 0,
    ADD COLUMN idempotency_key TEXT;
UPDATE orders o SET instrument_id = i.id FROM instruments i WHERE i.quik_symbol = o.symbol AND i.board = o.board;
ALTER TABLE orders ALTER COLUMN instrument_id SET NOT NULL;
CREATE UNIQUE INDEX orders_broker_idempotency_key ON orders (broker_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
ALTER TABLE orders DROP COLUMN symbol, DROP COLUMN currency;

CREATE TABLE quik_firms (
    firm_id TEXT PRIMARY KEY,
    exchange TEXT NOT NULL DEFAULT '',
    firm_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    status_flag INT,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    exec_id TEXT NOT NULL,
    last_qty NUMERIC(24, 8) NOT NULL,
    last_px NUMERIC(24, 8) NOT NULL,
    cum_qty NUMERIC(24, 8) NOT NULL,
    leaves_qty NUMERIC(24, 8) NOT NULL,
    adapter_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    execution_date DATE,
    trade_match_id TEXT NOT NULL DEFAULT '',
    contra_broker TEXT NOT NULL DEFAULT '',
    contra_trader TEXT NOT NULL DEFAULT '',
    CONSTRAINT executions_order_date_exec_key UNIQUE NULLS NOT DISTINCT (order_id, execution_date, exec_id)
);

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
    UNIQUE (trade_id, side)
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

ALTER TABLE execution_inbox_quarantine
    DROP CONSTRAINT execution_inbox_quarantine_order_id_fkey,
    ADD CONSTRAINT execution_inbox_quarantine_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id);
ALTER TABLE outbox_reconciliations
    DROP CONSTRAINT outbox_reconciliations_event_id_fkey,
    DROP CONSTRAINT outbox_reconciliations_order_id_fkey,
    DROP CONSTRAINT outbox_reconciliations_evidence_event_id_fkey,
    ADD CONSTRAINT outbox_reconciliations_event_id_fkey FOREIGN KEY (event_id) REFERENCES outbox_events(id),
    ADD CONSTRAINT outbox_reconciliations_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id),
    ADD CONSTRAINT outbox_reconciliations_evidence_event_id_fkey FOREIGN KEY (evidence_event_id) REFERENCES order_events(id);
ALTER TABLE order_events
    DROP CONSTRAINT order_events_order_id_fkey,
    ADD CONSTRAINT order_events_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id);

COMMIT;
