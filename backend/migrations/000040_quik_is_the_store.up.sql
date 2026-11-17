-- Поручение пользователя 02.11.2026: сделки, инструменты и фирмы живут только в
-- БД QUIK. Заявка хранится здесь, лишь пока QUIK её не закрыл: для отмены нужен
-- наш ClOrdID (FIX 41), которого в QUIK Orders нет.
BEGIN;

ALTER TABLE orders
    ADD COLUMN symbol TEXT NOT NULL DEFAULT '',
    ADD COLUMN currency TEXT NOT NULL DEFAULT '';
UPDATE orders o SET symbol = i.quik_symbol, board = COALESCE(NULLIF(o.board, ''), i.board), currency = i.currency
FROM instruments i WHERE i.id = o.instrument_id;

ALTER TABLE order_events
    DROP CONSTRAINT order_events_order_id_fkey,
    ADD CONSTRAINT order_events_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE;
ALTER TABLE outbox_reconciliations
    DROP CONSTRAINT outbox_reconciliations_event_id_fkey,
    DROP CONSTRAINT outbox_reconciliations_order_id_fkey,
    DROP CONSTRAINT outbox_reconciliations_evidence_event_id_fkey,
    ADD CONSTRAINT outbox_reconciliations_event_id_fkey FOREIGN KEY (event_id) REFERENCES outbox_events(id) ON DELETE CASCADE,
    ADD CONSTRAINT outbox_reconciliations_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
    ADD CONSTRAINT outbox_reconciliations_evidence_event_id_fkey FOREIGN KEY (evidence_event_id) REFERENCES order_events(id) ON DELETE CASCADE;
-- Дело карантина хранит сам отчёт; оно переживает удаление заявки.
ALTER TABLE execution_inbox_quarantine
    DROP CONSTRAINT execution_inbox_quarantine_order_id_fkey,
    ADD CONSTRAINT execution_inbox_quarantine_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE SET NULL;

DROP TABLE trade_side_reports;
DROP TABLE trade_sides;
DROP TABLE trade_reports;
DROP TABLE trades;
DROP TABLE trade_sync_runs;
DROP TABLE executions;
DROP TABLE quik_firms;

ALTER TABLE orders
    DROP COLUMN instrument_id,
    DROP COLUMN account_id,
    DROP COLUMN avg_price,
    DROP COLUMN idempotency_key;
DROP TABLE instruments;

-- То же правило, что у orders.Store.PurgeFinished, но без выдержки: ответы на
-- эти заявки давно отправлены.
CREATE TEMP TABLE finished_orders ON COMMIT DROP AS
SELECT id FROM orders
WHERE status IN ('FILLED','CANCELLED','EXPIRED','REJECTED','FAILED')
   OR (status IN ('ACKNOWLEDGED','PARTIALLY_FILLED','CANCEL_PENDING')
       AND (created_at AT TIME ZONE 'Asia/Bishkek')::date < (now() AT TIME ZONE 'Asia/Bishkek')::date);
DELETE FROM outbox_events e USING finished_orders f WHERE e.aggregate_type = 'order' AND e.aggregate_id = f.id::text;
DELETE FROM orders o USING finished_orders f WHERE o.id = f.id;

COMMIT;
