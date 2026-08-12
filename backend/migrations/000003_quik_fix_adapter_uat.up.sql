-- Wire Gateway to QuikFIXAdapterService1 on 192.168.0.89 (UAT).
-- Adapter IN sessions (acceptor): SenderCompID=QUIK / TargetCompID=<client>
-- Gateway initiator must invert: SenderCompID=<client>, TargetCompID=QUIK

-- Keep local simulators available but disabled for real-adapter UAT.
UPDATE adapters SET enabled = false, updated_at = now()
WHERE id IN ('adapter-1', 'adapter-2');

INSERT INTO adapters (
    id, name, type, host, port, begin_string,
    sender_comp_id, target_comp_id,
    heartbeat_interval, reset_on_logon,
    weight, priority, max_concurrent_orders, enabled
) VALUES
    (
        'quik-fix-1',
        'Quik FIX Adapter Service 1 (primary)',
        'arqa',
        '192.168.0.89',
        15101,
        'FIX.4.2',
        'TEST',
        'QUIK',
        30,
        true,
        100,
        200,
        1000,
        true
    ),
    (
        'quik-fix-1b',
        'Quik FIX Adapter Service 1 (secondary)',
        'arqa',
        '192.168.0.89',
        15103,
        'FIX.4.2',
        'BROKER2',
        'QUIK',
        30,
        true,
        50,
        100,
        1000,
        false
    )
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    type = EXCLUDED.type,
    host = EXCLUDED.host,
    port = EXCLUDED.port,
    begin_string = EXCLUDED.begin_string,
    sender_comp_id = EXCLUDED.sender_comp_id,
    target_comp_id = EXCLUDED.target_comp_id,
    heartbeat_interval = EXCLUDED.heartbeat_interval,
    reset_on_logon = EXCLUDED.reset_on_logon,
    weight = EXCLUDED.weight,
    priority = EXCLUDED.priority,
    max_concurrent_orders = EXCLUDED.max_concurrent_orders,
    enabled = EXCLUDED.enabled,
    updated_at = now();
