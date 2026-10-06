-- Runtime settings that operators can change from the admin UI
-- (e.g. QUIK Module API host, which moves between machines).
CREATE TABLE IF NOT EXISTS gateway_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT NOT NULL DEFAULT ''
);

INSERT INTO gateway_settings (key, value)
VALUES ('quik_api_base_url', 'http://192.168.0.89:5000')
ON CONFLICT (key) DO NOTHING;
