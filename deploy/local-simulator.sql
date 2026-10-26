-- Run only against the isolated fix_gateway_local database, after migrations.
BEGIN;
DO $$
BEGIN
  IF current_database() <> 'fix_gateway_local' THEN
    RAISE EXCEPTION 'Simulator bootstrap requires fix_gateway_local database';
  END IF;
END $$;

-- Migrations seed real UAT adapters. Disable every adapter before enabling local ones.
UPDATE adapters SET enabled = false, updated_at = now();
INSERT INTO adapters (
    id, name, type, host, port, begin_string, sender_comp_id, target_comp_id,
    reset_on_logon, enabled, priority, weight
) VALUES
    ('adapter-1', 'Local FIX Simulator 1', 'simulator', 'fixsimulator', 5001,
     'FIX.4.4', 'GATEWAY1', 'SIM1', true, true, 100, 100),
    ('adapter-2', 'Local FIX Simulator 2', 'simulator', 'fixsimulator', 5002,
     'FIX.4.4', 'GATEWAY2', 'SIM2', true, true, 100, 50)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, type = EXCLUDED.type, host = EXCLUDED.host,
    port = EXCLUDED.port, begin_string = EXCLUDED.begin_string,
    sender_comp_id = EXCLUDED.sender_comp_id, target_comp_id = EXCLUDED.target_comp_id,
    reset_on_logon = true, enabled = true, priority = EXCLUDED.priority,
    weight = EXCLUDED.weight, username_env = '', password_env = '', updated_at = now();

-- Override the migration's real QUIK HTTP origin as well as the process default.
INSERT INTO gateway_settings (key, value) VALUES ('quik_api_base_url', 'http://127.0.0.1:1')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

INSERT INTO brokers (code, name) VALUES ('LOCAL_DEMO', 'Local Simulator Demo')
ON CONFLICT (code) DO NOTHING;
INSERT INTO instruments (internal_symbol, quik_symbol, board, currency, security_type)
VALUES ('REJECT', 'REJECT', 'TQBR', 'RUB', 'CS')
ON CONFLICT (internal_symbol) DO NOTHING;
COMMIT;
