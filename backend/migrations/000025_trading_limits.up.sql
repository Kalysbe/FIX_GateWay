CREATE TABLE trading_limits (
    broker_id BIGINT PRIMARY KEY CHECK (broker_id >= 0),
    paused BOOLEAN NOT NULL DEFAULT false,
    max_quantity NUMERIC CHECK (max_quantity > 0),
    max_open_orders INT CHECK (max_open_orders BETWEEN 1 AND 1000000),
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- broker_id=0 applies to every broker. Both global and broker limits apply.
INSERT INTO trading_limits(broker_id) VALUES(0);
CREATE TABLE trading_limit_history (
    id BIGSERIAL PRIMARY KEY,
    broker_id BIGINT NOT NULL,
    actor_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    before_policy JSONB,
    after_policy JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
