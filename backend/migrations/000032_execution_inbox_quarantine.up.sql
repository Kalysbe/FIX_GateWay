-- Operator cases for incoming FIX reports that could not be applied and were
-- set aside so that trading can resume. The entry file is kept on the inbox
-- volume under quarantine/; the report itself is kept here as evidence.
CREATE TABLE execution_inbox_quarantine (
    entry_id        UUID PRIMARY KEY,
    entry_name      TEXT NOT NULL,
    received_at     TIMESTAMPTZ,
    adapter_id      TEXT NOT NULL DEFAULT '',
    cl_ord_id       TEXT NOT NULL DEFAULT '',
    exec_id         TEXT NOT NULL DEFAULT '',
    exec_type       TEXT NOT NULL DEFAULT '',
    order_id        UUID REFERENCES orders(id),
    report          JSONB,
    apply_error     TEXT NOT NULL,
    failed_at       TIMESTAMPTZ,
    attempts        INTEGER NOT NULL DEFAULT 0,
    reason          TEXT NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 2000),
    quarantined_by  TEXT NOT NULL,
    quarantined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_by     TEXT,
    resolved_at     TIMESTAMPTZ,
    resolution_note TEXT CHECK (resolution_note IS NULL OR length(btrim(resolution_note)) BETWEEN 1 AND 2000),
    CHECK ((resolved_at IS NULL) = (resolved_by IS NULL) AND (resolved_at IS NULL) = (resolution_note IS NULL))
);
CREATE INDEX idx_execution_inbox_quarantine_open ON execution_inbox_quarantine(quarantined_at DESC) WHERE resolved_at IS NULL;
CREATE INDEX idx_execution_inbox_quarantine_order ON execution_inbox_quarantine(order_id) WHERE order_id IS NOT NULL;
