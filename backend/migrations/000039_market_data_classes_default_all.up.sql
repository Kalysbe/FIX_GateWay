-- No chosen classes now serves every class. Only the untouched LISTBND default of
-- migration 000033 is dropped; a choice made by an administrator stays.
DELETE FROM gateway_settings
WHERE key = 'market_data_classes' AND value = 'LISTBND' AND updated_by = '';
