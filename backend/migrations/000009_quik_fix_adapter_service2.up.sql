-- Wire Gateway to QuikFIXAdapterService2 on 192.168.0.89:15102 (UAT).
-- Adapter IN session: SenderCompID=QUIK / TargetCompID=TEST
-- Gateway initiator must invert: SenderCompID=TEST, TargetCompID=QUIK
-- QuikApi User/Password (quikfix2) belong to the Windows service, not FIX Logon.

INSERT INTO adapters (
    id, name, type, host, port, begin_string,
    sender_comp_id, target_comp_id,
    heartbeat_interval, reset_on_logon,
    weight, priority, max_concurrent_orders, enabled
) VALUES (
    'quik-fix-2',
    'Quik FIX Adapter Service 2',
    'arqa',
    '192.168.0.89',
    15102,
    'FIX.4.2',
    'TEST',
    'QUIK',
    30,
    true,
    100,
    150,
    1000,
    true
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
