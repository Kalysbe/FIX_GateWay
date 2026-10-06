-- Run only against an isolated database after all migrations.
BEGIN;
DO $$
DECLARE snapshot jsonb;
BEGIN
 snapshot := gateway_dashboard_compute();
 IF snapshot#>>'{stats,total}' <> '0' OR snapshot#>>'{observability,success_rate}' <> '100' THEN
  RAISE EXCEPTION 'invalid empty snapshot: %',snapshot;
 END IF;
END $$;
INSERT INTO brokers(id,code,name) VALUES (900001,'DASHBOARD-TEST','Test broker');
INSERT INTO broker_accounts(id,broker_id,code,trading_account) VALUES (900001,900001,'TEST','TEST');
INSERT INTO instruments(id,internal_symbol,quik_symbol) VALUES (900001,'DASHBOARD-TEST','TEST');
INSERT INTO orders(broker_id,account_id,instrument_id,client_order_id,side,order_type,quantity,status,created_at)
 SELECT 900001,900001,900001,'dashboard-'||n,'BUY','MARKET',1,status,now()
 FROM (VALUES (1,'FILLED'),(2,'REJECTED'),(3,'FAILED'),(4,'QUEUED'),(5,'ACKNOWLEDGED'),(6,'PARTIALLY_FILLED'),(7,'CANCEL_PENDING')) AS fixture(n,status);
INSERT INTO orders(broker_id,account_id,instrument_id,client_order_id,side,order_type,quantity,status,created_at)
 VALUES (900001,900001,900001,'yesterday','BUY','MARKET',1,'FILLED',date_trunc('day',now())-interval '1 second');
INSERT INTO obs_events(ts,event_type,subtype,result,broker_id,response_time_ms,error_code) VALUES
 (now(),'API_REQUEST','ORDER_CREATE','SUCCESS',900001,10,NULL),
 (now(),'API_REQUEST','ORDER_CREATE','FAILURE',900001,100,'ORDER_FAILED'),
 (now(),'ERROR','ORDER_FAILED',NULL,900001,NULL,'ORDER_FAILED'),
 (date_trunc('day',now())-interval '1 second','API_REQUEST','ORDER_CREATE','SUCCESS',900001,900,NULL),
 (now(),'AUTH','AUTH_FAILURE','FAILURE',NULL,NULL,'INVALID_AUTH');
DO $$
DECLARE snapshot jsonb;
BEGIN
 snapshot:=gateway_dashboard_compute();
 IF snapshot#>>'{observability,requests_today}' <> '2'
 OR (snapshot#>>'{observability,avg_latency_ms}')::numeric <> 55
 OR (snapshot#>>'{observability,success_rate}')::numeric <> 50
 OR (snapshot#>>'{top_brokers,0,success_rate}')::numeric <> 50
 OR snapshot#>>'{connected_brokers,0,requests}' <> '2'
 OR snapshot#>>'{observability,connected_now}' <> '1'
 OR snapshot#>>'{top_errors,0,count}' <> '2'
 OR snapshot#>>'{observability,auth_failures_today}' <> '1'
 OR snapshot#>>'{stats,total}' <> '7'
 OR snapshot#>>'{stats,filled}' <> '1'
 OR snapshot#>>'{stats,rejected}' <> '2'
 OR snapshot#>>'{stats,active}' <> '4'
 OR snapshot#>>'{active_accounts}' <> '1'
 THEN RAISE EXCEPTION 'invalid populated snapshot: %',snapshot; END IF;
END $$;
UPDATE gateway_dashboard_cache SET refreshed_at=now()-interval '10 minutes';
SELECT gateway_refresh_dashboard_snapshot();
DO $$
BEGIN
 IF gateway_dashboard_snapshot()#>>'{stats,total}' <> '7' THEN RAISE EXCEPTION 'cache refresh failed'; END IF;
END $$;
ROLLBACK;
