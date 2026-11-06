INSERT INTO gateway_settings (key, value)
VALUES ('quik_api_base_url', 'http://172.23.10.84:5000')
ON CONFLICT (key) DO NOTHING;
