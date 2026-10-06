CREATE TABLE outbox_reconciliations (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),
    order_id UUID NOT NULL REFERENCES orders(id),
    evidence_event_id BIGINT NOT NULL REFERENCES order_events(id),
    actor_id TEXT NOT NULL,
    note TEXT NOT NULL CHECK (length(btrim(note)) BETWEEN 1 AND 2000),
    previous_error TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_reconciliation_queue ON outbox_events(id)
    WHERE status='FAILED' AND last_error LIKE 'RECONCILIATION_REQUIRED:%';
