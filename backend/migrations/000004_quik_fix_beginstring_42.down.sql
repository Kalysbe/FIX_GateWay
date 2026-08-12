UPDATE adapters
SET begin_string = 'FIX.4.4',
    reset_on_logon = false,
    updated_at = now()
WHERE id IN ('quik-fix-1', 'quik-fix-1b');
