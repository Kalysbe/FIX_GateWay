INSERT INTO gateway_settings (key, value)
VALUES ('market_data_classes', 'LISTBND')
ON CONFLICT (key) DO NOTHING;
