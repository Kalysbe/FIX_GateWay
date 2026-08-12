-- API token security: permissions, audit fields, IP whitelist, rate limits

ALTER TABLE api_tokens
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS revoked_by UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS rotated_from UUID REFERENCES api_tokens(id);

-- Default full permissions for existing tokens that have an empty array
UPDATE api_tokens
SET permissions = '[
  "orders:create",
  "orders:read",
  "orders:cancel",
  "orders:replace",
  "trades:read",
  "positions:read"
]'::jsonb
WHERE permissions = '[]'::jsonb OR permissions IS NULL;

CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_broker_status ON api_tokens(broker_id, status);
CREATE INDEX IF NOT EXISTS idx_api_tokens_status ON api_tokens(status);

ALTER TABLE brokers
    ADD COLUMN IF NOT EXISTS ip_whitelist_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS rate_limit_rps INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS rate_limit_orders_per_sec INT NOT NULL DEFAULT 20;

CREATE TABLE IF NOT EXISTS broker_ip_whitelist (
    id         BIGSERIAL PRIMARY KEY,
    broker_id  BIGINT NOT NULL REFERENCES brokers(id) ON DELETE CASCADE,
    ip_cidr    TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (broker_id, ip_cidr)
);

CREATE INDEX IF NOT EXISTS idx_broker_ip_whitelist_broker ON broker_ip_whitelist(broker_id);

CREATE TABLE IF NOT EXISTS auth_failures (
    id         BIGSERIAL PRIMARY KEY,
    ip         TEXT NOT NULL,
    endpoint   TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL DEFAULT '',
    token_id   UUID,
    broker_id  BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_auth_failures_ip_created ON auth_failures(ip, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_auth_failures_created ON auth_failures(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action_created ON audit_logs(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_broker_created ON audit_logs(broker_id, created_at DESC);
