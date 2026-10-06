import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {setTimeout as delay} from 'node:timers/promises';

const compose=['compose','-p','fix-gateway-verification','-f','deploy/docker-compose.local.yml','-f','deploy/docker-compose.verification.yml'];
const docker=process.env.DOCKER_BIN || 'docker';
const base='http://127.0.0.1:38080';
const command=args=>execFileSync(docker,[...compose,...args],{encoding:'utf8',timeout:60000});
const config=JSON.parse(command(['config','--format','json']));
assert.equal(config.name,'fix-gateway-verification');
assert.equal(config.networks.default.internal,true);
assert.equal(config.services.gateway.environment.ADAPTER_HOST_OVERRIDE,'fixsimulator');
const containers=JSON.parse(command(['ps','--format','json','access']));
assert.equal(containers.Project,'fix-gateway-verification');
assert.ok(containers.Publishers.some(p=>p.PublishedPort===38080 && p.TargetPort===8080));
function sql(statement){return execFileSync(docker,[...compose,'exec','-T','postgres','psql','-U','gateway','-d','fix_gateway_local','-v','ON_ERROR_STOP=1','-At'],{input:statement,encoding:'utf8',timeout:15000}).trim();}
async function call(path,token='',method='GET',body,status=200){
 const response=await fetch(base+path,{method,headers:{...(token?{Authorization:`Bearer ${token}`} : {}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(10000)});
 const value=await response.json();assert.equal(response.status,status,`${method} ${path}: ${JSON.stringify(value)}`);return value;
}
async function until(fn){const deadline=Date.now()+45000;while(Date.now()<deadline){try{const value=await fn();if(value)return value;}catch{}await delay(200);}throw Error('Verification timed out');}
assert.equal(sql('SELECT version FROM schema_migrations'),'25');
const {token:admin}=await call('/api/v1/admin/login','','POST',{email:'admin@localhost',password:'admin123'});
const {broker}=await call('/api/v1/admin/brokers',admin,'POST',{code:`VERIFY-${randomUUID()}`,name:'Release verification'},201);
await call(`/api/v1/admin/brokers/${broker.id}/accounts`,admin,'POST',{code:'VERIFY',name:'Test',quik_firm:'SIM',trading_account:'VERIFY'},201);
const {token}=await call(`/api/v1/admin/brokers/${broker.id}/tokens`,admin,'POST',{name:'Verification',quik_firm:'SIM',expires_in_days:30},201);
const payload={account_id:'VERIFY',client_code:'VERIFY',client_order_id:randomUUID(),symbol:'SBER',board:'TQBR',side:'BUY',order_type:'LIMIT',quantity:'1',price:'100'};
const {order}=await call('/api/v1/orders',token,'POST',payload,201);
await until(async()=>{const value=await call(`/api/v1/orders/${order.id}`,token);return value.order.status==='ACKNOWLEDGED';});
// Deterministic crash-window injection: venue ACK was received, but queue completion
// was lost. This models durable PROCESSING state; it is not a socket-loss test.
sql(`UPDATE outbox_events SET status='PROCESSING',locked_by='crashed-verification-worker',locked_at=now()-interval '5 minutes' WHERE aggregate_id='${order.id}' AND event_type='ORDER_SEND';`);
command(['restart','gateway']);
await until(async()=>{const value=await call(`/api/v1/admin/reconciliation?order_id=${order.id}`,admin);return value.cases.find(c=>c.resolved_at && c.actor_id==='system:fix-reconciliation');});
assert.equal(sql(`SELECT count(*) FROM outbox_reconciliations WHERE order_id='${order.id}'`),'1');
assert.equal(sql(`SELECT attempts FROM outbox_events WHERE aggregate_id='${order.id}' AND event_type='ORDER_SEND'`),'1');
assert.equal(sql(`SELECT fix_send_attempted_at IS NOT NULL FROM orders WHERE id='${order.id}'`),'t');
const replay=await call('/api/v1/orders',token,'POST',payload);assert.equal(replay.order.id,order.id);assert.equal(replay.idempotent,true);
// Real socket fault: simulator accepts D, drops the connection before ACK and
// emits the retained ACK after reconnect. Durable crash-window state is injected
// separately so both the unconfirmed and confirmed reconciliation paths run.
sql("INSERT INTO instruments(internal_symbol,quik_symbol,board,currency) VALUES('FAULT_RECOVER','FAULT_RECOVER','TQBR','RUB') ON CONFLICT(internal_symbol) DO NOTHING;");
const fault=await call('/api/v1/orders',token,'POST',{...payload,symbol:'FAULT_RECOVER',client_order_id:randomUUID()},201);
const clOrdID=sql(`SELECT fix_cl_ord_id FROM orders WHERE id='${fault.order.id}'`);
await until(()=>command(['logs','--no-color','fixsimulator']).includes(`FAULT_RECEIVED ${clOrdID}`));
sql(`UPDATE outbox_events SET status='PROCESSING',locked_by='fault-window',locked_at=now()-interval '5 minutes' WHERE aggregate_id='${fault.order.id}' AND event_type='ORDER_SEND';`);
await until(async()=>{const value=await call(`/api/v1/admin/reconciliation?order_id=${fault.order.id}`,admin);return value.cases.find(c=>!c.resolved_at && !c.evidence_event_id);});
await until(async()=>{const value=await call(`/api/v1/admin/reconciliation?order_id=${fault.order.id}`,admin);return value.cases.find(c=>c.resolved_at && c.evidence_event_id && c.actor_id==='system:fix-reconciliation');});
assert.equal(sql(`SELECT attempts FROM outbox_events WHERE aggregate_id='${fault.order.id}' AND event_type='ORDER_SEND'`),'1');
const logs=command(['logs','--no-color','fixsimulator']);
assert.equal(logs.split(`FAULT_RECEIVED ${clOrdID}`).length-1,1,'uncertain order was transmitted more than once');
await call('/api/v1/admin/trading-limits',admin,'PUT',{broker_id:broker.id,paused:true,max_quantity:'2',max_open_orders:3,revision:0,reason:'Release verification pause'});
const rejected=await call('/api/v1/orders',token,'POST',{...payload,client_order_id:randomUUID()},422);assert.equal(rejected.code,'TRADING_PAUSED');
const history=await call(`/api/v1/admin/trading-limits/history?broker_id=${broker.id}`,admin);assert.equal(history.history[0].after.paused,true);
command(['restart','gateway']);
await until(async()=>{const value=await call('/api/v1/admin/trading-limits',admin);return value.policies.some(p=>p.broker_id===broker.id && p.paused);});
// Restore into a separate disposable database, never overwrite the source.
command(['stop','gateway']);
const snapshotSQL=`SELECT json_build_object('orders',(SELECT count(*) FROM orders),'executions',(SELECT count(*) FROM executions),'outbox',(SELECT count(*) FROM outbox_events),'reconciliations',(SELECT count(*) FROM outbox_reconciliations),'history',(SELECT count(*) FROM trading_limit_history),'fences',(SELECT count(*) FROM orders WHERE fix_send_attempted_at IS NOT NULL))::text`;
const snapshot=sql(snapshotSQL);
const restoreDB=`restore_${randomUUID().replaceAll('-','')}`;
sql(`CREATE DATABASE ${restoreDB}`);
command(['exec','-T','postgres','pg_dump','-U','gateway','-d','fix_gateway_local','-Fc','-f','/tmp/verification.dump']);
command(['exec','-T','postgres','pg_restore','-U','gateway','-d',restoreDB,'--no-owner','--exit-on-error','/tmp/verification.dump']);
const restoredSnapshot=command(['exec','-T','postgres','psql','-U','gateway','-d',restoreDB,'-At','-c',snapshotSQL]).trim();
assert.deepEqual(JSON.parse(restoredSnapshot),JSON.parse(snapshot));
const restoredContainer='fix-gateway-verification-restored-gateway';
try {
 // The restored clone must not run against the same FIX store concurrently.
 command(['stop','gateway']);
 command(['run','-d','--no-deps','--name',restoredContainer,'-e',`DATABASE_URL=postgres://gateway:local-simulator-only@postgres:5432/${restoreDB}?sslmode=disable`,'gateway']);
 await until(()=>{try{return execFileSync(docker,['exec',restoredContainer,'wget','-qO-','http://127.0.0.1:8080/ready'],{encoding:'utf8',timeout:5000}).includes('ready');}catch{return false;}});
 assert.equal(command(['exec','-T','postgres','psql','-U','gateway','-d',restoreDB,'-At','-c',`SELECT paused FROM trading_limits WHERE broker_id=${broker.id}`]).trim(),'t');
 assert.equal(command(['exec','-T','postgres','psql','-U','gateway','-d',restoreDB,'-At','-c',`SELECT attempts FROM outbox_events WHERE aggregate_id='${fault.order.id}' AND event_type='ORDER_SEND'`]).trim(),'1');
} finally {
 execFileSync(docker,['rm','-f',restoredContainer],{encoding:'utf8',timeout:15000});
 command(['start','gateway']);
}
// A marker in the FIX volume must survive replacement of the gateway container.
command(['exec','-T','gateway','sh','-c','test -d /app/fix-sessions/adapter-1 && touch /app/fix-sessions/.verification-marker']);
command(['up','-d','--no-deps','--force-recreate','gateway']);
await until(async()=>{await call('/ready');return true;});
command(['exec','-T','gateway','test','-f','/app/fix-sessions/.verification-marker']);
console.log('PASS migrations / FIX socket disconnect / late confirmation / automatic reconciliation / single wire order / policy history / backup restore / restored readiness / persistent FIX store after container replacement');
