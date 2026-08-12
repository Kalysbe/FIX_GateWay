CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE roles (
    id          SMALLSERIAL PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

INSERT INTO roles (code, description) VALUES
    ('SUPER_ADMIN', 'Full access'),
    ('ADMIN', 'Brokers, tokens, adapters, orders'),
    ('OPERATOR', 'Orders and monitoring'),
    ('AUDITOR', 'Audit and logs only'),
    ('READ_ONLY', 'Read-only access');

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name     TEXT NOT NULL DEFAULT '',
    role_code     TEXT NOT NULL REFERENCES roles(code),
    status        TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE brokers (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'BLOCKED', 'DISABLED')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    broker_id    BIGINT NOT NULL REFERENCES brokers(id),
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED', 'DISABLED')),
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_used_ip TEXT
);

CREATE INDEX idx_api_tokens_broker ON api_tokens(broker_id);

CREATE TABLE broker_accounts (
    id              BIGSERIAL PRIMARY KEY,
    broker_id       BIGINT NOT NULL REFERENCES brokers(id),
    code            TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    quik_firm       TEXT NOT NULL DEFAULT '',
    trading_account TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DISABLED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (broker_id, code)
);

CREATE TABLE instruments (
    id             BIGSERIAL PRIMARY KEY,
    internal_symbol TEXT NOT NULL UNIQUE,
    quik_symbol    TEXT NOT NULL,
    board          TEXT NOT NULL DEFAULT '',
    currency       TEXT NOT NULL DEFAULT 'RUB',
    security_type  TEXT NOT NULL DEFAULT 'CS',
    status         TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE broker_instrument_mapping (
    id               BIGSERIAL PRIMARY KEY,
    broker_id        BIGINT NOT NULL REFERENCES brokers(id),
    broker_symbol    TEXT NOT NULL,
    instrument_id    BIGINT NOT NULL REFERENCES instruments(id),
    UNIQUE (broker_id, broker_symbol)
);

CREATE TABLE adapters (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    type                   TEXT NOT NULL CHECK (type IN ('arqa', 'simulator')),
    host                   TEXT NOT NULL,
    port                   INT NOT NULL,
    begin_string           TEXT NOT NULL DEFAULT 'FIX.4.4',
    sender_comp_id         TEXT NOT NULL,
    target_comp_id         TEXT NOT NULL,
    heartbeat_interval     INT NOT NULL DEFAULT 30,
    reset_on_logon         BOOLEAN NOT NULL DEFAULT false,
    username_env           TEXT NOT NULL DEFAULT '',
    password_env           TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'UNKNOWN',
    priority               INT NOT NULL DEFAULT 100,
    weight                 INT NOT NULL DEFAULT 100,
    max_concurrent_orders  INT NOT NULL DEFAULT 1000,
    enabled                BOOLEAN NOT NULL DEFAULT true,
    active_orders          BIGINT NOT NULL DEFAULT 0,
    processed_orders       BIGINT NOT NULL DEFAULT 0,
    error_count            BIGINT NOT NULL DEFAULT 0,
    last_heartbeat_at      TIMESTAMPTZ,
    started_at             TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE adapter_events (
    id          BIGSERIAL PRIMARY KEY,
    adapter_id  TEXT NOT NULL REFERENCES adapters(id),
    event_type  TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    broker_id         BIGINT NOT NULL REFERENCES brokers(id),
    account_id        BIGINT NOT NULL REFERENCES broker_accounts(id),
    client_order_id   TEXT NOT NULL,
    instrument_id     BIGINT NOT NULL REFERENCES instruments(id),
    side              TEXT NOT NULL CHECK (side IN ('BUY', 'SELL')),
    order_type        TEXT NOT NULL CHECK (order_type IN ('MARKET', 'LIMIT')),
    time_in_force     TEXT NOT NULL DEFAULT 'DAY',
    quantity          NUMERIC(24, 8) NOT NULL,
    price             NUMERIC(24, 8),
    filled_quantity   NUMERIC(24, 8) NOT NULL DEFAULT 0,
    avg_price         NUMERIC(24, 8) NOT NULL DEFAULT 0,
    status            TEXT NOT NULL,
    adapter_id        TEXT REFERENCES adapters(id),
    fix_cl_ord_id     TEXT,
    quik_order_id     TEXT,
    reject_reason     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (broker_id, client_order_id)
);

CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_broker_created ON orders(broker_id, created_at DESC);
CREATE INDEX idx_orders_fix_cl_ord_id ON orders(fix_cl_ord_id);

CREATE TABLE order_events (
    id         BIGSERIAL PRIMARY KEY,
    order_id   UUID NOT NULL REFERENCES orders(id),
    event_type TEXT NOT NULL,
    from_status TEXT,
    to_status  TEXT,
    adapter_id TEXT,
    message    TEXT NOT NULL DEFAULT '',
    payload    JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_order_events_order ON order_events(order_id, created_at);

CREATE TABLE executions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     UUID NOT NULL REFERENCES orders(id),
    exec_id      TEXT NOT NULL,
    last_qty     NUMERIC(24, 8) NOT NULL,
    last_px      NUMERIC(24, 8) NOT NULL,
    cum_qty      NUMERIC(24, 8) NOT NULL,
    leaves_qty   NUMERIC(24, 8) NOT NULL,
    adapter_id   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (exec_id)
);

CREATE TABLE outbox_events (
    id             BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id   TEXT NOT NULL,
    event_type     TEXT NOT NULL,
    payload        JSONB NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING', 'PROCESSING', 'DONE', 'FAILED')),
    attempts       INT NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by      TEXT,
    locked_at      TIMESTAMPTZ,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_outbox_pending ON outbox_events(status, available_at)
    WHERE status IN ('PENDING', 'PROCESSING');

CREATE TABLE audit_logs (
    id                BIGSERIAL PRIMARY KEY,
    actor_type        TEXT NOT NULL,
    actor_id          TEXT,
    broker_id         BIGINT,
    action            TEXT NOT NULL,
    entity_type       TEXT NOT NULL,
    entity_id         TEXT,
    ip                TEXT,
    client_order_id   TEXT,
    internal_order_id TEXT,
    adapter_id        TEXT,
    request_payload   JSONB,
    result            TEXT,
    error             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_created ON audit_logs(created_at DESC);

CREATE TABLE api_logs (
    id            BIGSERIAL PRIMARY KEY,
    method        TEXT NOT NULL,
    path          TEXT NOT NULL,
    status_code   INT NOT NULL,
    broker_id     BIGINT,
    user_id       UUID,
    ip            TEXT,
    duration_ms   INT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed admin user password: admin123 (bcrypt hash generated at runtime seed if missing)
-- Seed adapters for local simulator (two instances)
INSERT INTO adapters (id, name, type, host, port, sender_comp_id, target_comp_id, reset_on_logon, weight, priority)
VALUES
    ('adapter-1', 'FIX Simulator 1', 'simulator', 'fixsimulator', 5001, 'GATEWAY1', 'SIM1', true, 100, 100),
    ('adapter-2', 'FIX Simulator 2', 'simulator', 'fixsimulator', 5002, 'GATEWAY2', 'SIM2', true, 50, 100);

INSERT INTO instruments (internal_symbol, quik_symbol, board, currency, security_type)
VALUES ('SBER', 'SBER', 'TQBR', 'RUB', 'CS'),
       ('GAZP', 'GAZP', 'TQBR', 'RUB', 'CS');
