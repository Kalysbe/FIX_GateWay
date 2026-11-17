-- QUIK trades are read directly from the QUIK database (public."Trades");
-- the QUIK Module API address is no longer used.
DELETE FROM gateway_settings WHERE key = 'quik_api_base_url';
