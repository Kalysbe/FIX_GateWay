CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_quik_order_lookup ON orders(quik_order_id) WHERE quik_order_id IS NOT NULL;
