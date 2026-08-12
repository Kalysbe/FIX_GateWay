ALTER TABLE orders
    DROP COLUMN IF EXISTS fix_firm,
    DROP COLUMN IF EXISTS fix_client_id;

ALTER TABLE api_tokens
    DROP COLUMN IF EXISTS quik_firm;
