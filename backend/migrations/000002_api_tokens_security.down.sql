DROP INDEX IF EXISTS idx_audit_broker_created;
DROP INDEX IF EXISTS idx_audit_action_created;
DROP INDEX IF EXISTS idx_auth_failures_created;
DROP INDEX IF EXISTS idx_auth_failures_ip_created;
DROP TABLE IF EXISTS auth_failures;

DROP INDEX IF EXISTS idx_broker_ip_whitelist_broker;
DROP TABLE IF EXISTS broker_ip_whitelist;

ALTER TABLE brokers
    DROP COLUMN IF EXISTS rate_limit_orders_per_sec,
    DROP COLUMN IF EXISTS rate_limit_rps,
    DROP COLUMN IF EXISTS ip_whitelist_enabled;

DROP INDEX IF EXISTS idx_api_tokens_status;
DROP INDEX IF EXISTS idx_api_tokens_broker_status;
DROP INDEX IF EXISTS idx_api_tokens_hash;

ALTER TABLE api_tokens
    DROP COLUMN IF EXISTS rotated_from,
    DROP COLUMN IF EXISTS revoked_by,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS permissions,
    DROP COLUMN IF EXISTS description;
