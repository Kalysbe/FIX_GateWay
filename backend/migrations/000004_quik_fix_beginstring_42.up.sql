-- Quik FIX Adapter confirmed BeginString FIX.4.2 (Logon handshake on :15101).
UPDATE adapters
SET begin_string = 'FIX.4.2',
    reset_on_logon = true,
    updated_at = now()
WHERE id IN ('quik-fix-1', 'quik-fix-1b');
