-- QUIK classes whose Params rows are served as market data, comma-separated.
-- Administrators change the list from the "Текущие торги" page.
INSERT INTO gateway_settings (key, value)
VALUES ('market_data_classes', 'LISTBND')
ON CONFLICT (key) DO NOTHING;
