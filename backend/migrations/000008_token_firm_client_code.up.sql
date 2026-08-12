-- Firm code (FIX 5018) is bound to the API token, not derived from account_id.
ALTER TABLE api_tokens
    ADD COLUMN IF NOT EXISTS quik_firm TEXT NOT NULL DEFAULT '';

-- Persist FIX client code (109) and firm (5018) on the order at create time.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS fix_client_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS fix_firm TEXT NOT NULL DEFAULT '';
