DELETE FROM adapter_events WHERE adapter_id IN ('quik-fix-1', 'quik-fix-1b');
DELETE FROM adapters WHERE id IN ('quik-fix-1', 'quik-fix-1b');

UPDATE adapters SET enabled = true, updated_at = now()
WHERE id IN ('adapter-1', 'adapter-2');
