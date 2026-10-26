import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';

// Never target UAT: fixed loopback endpoint and verified isolated Compose project.
const base = 'http://127.0.0.1:28080';
const compose = ['compose', '-f', 'deploy/docker-compose.local.yml'];
const docker = process.env.DOCKER_BIN || 'docker';
function command(args) { return execFileSync(docker, args, { encoding: 'utf8', timeout: 60000 }); }
function sql(statement) {
  return execFileSync(docker, [...compose, 'exec', '-T', 'postgres', 'psql', '-U', 'gateway', '-d', 'fix_gateway_local', '-v', 'ON_ERROR_STOP=1', '-At'],
    { input: statement, encoding: 'utf8', timeout: 15000 }).trim();
}
async function call(path, token, method = 'GET', body) {
  const started = performance.now();
  const response = await fetch(base + path, {
    method, headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? {'Content-Type':'application/json'} : {}) },
    body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(20000),
  });
  return {status:response.status, body:await response.json(), ms:performance.now()-started,
    retryMs:Math.max(100, Number(response.headers.get('Retry-After') || 1) * 1000)};
}
async function expect(path, token, method, body, status=200) {
  const result=await call(path,token,method,body);
  assert.equal(result.status,status,`${method || 'GET'} ${path}: ${JSON.stringify(result.body)}`);
  return result.body;
}
async function waitHealth() {
  const deadline=Date.now()+60000;
  while(Date.now()<deadline) {
    try { if((await call('/health')).status===200) return; } catch {}
    await delay(500);
  }
  assert.fail('gateway did not recover');
}
const run=randomUUID();
const prefix=`RELIABILITY-${run}`;
const accepted=new Map();
const timings=[];
let token,admin,tokenId;
let simulatorStopped=false;
let databaseStopped=false;
const payload={account_id:'RELIABILITY',client_code:'RELIABILITY',symbol:'SBER',board:'TQBR',
  side:'BUY',order_type:'LIMIT',time_in_force:'DAY',quantity:'1',price:'100'};
async function submit(index) {
  const clientId=`${prefix}-${index}`;
  const body={...payload,client_order_id:clientId,side:index%2 ? 'SELL':'BUY'};
  const deadline=Date.now()+60000;
  while(Date.now()<deadline) {
    let result;
    try { result=await call('/api/v1/orders',token,'POST',body); }
    catch { await delay(500); continue; } // same idempotency key on uncertain HTTP result
    if(result.status===200 || result.status===201) {
      assert.match(result.body.order.id,/^[0-9a-f-]{36}$/i);
      accepted.set(clientId,result.body.order.id);
      timings.push(result.ms);
      return;
    }
    assert.ok([429,503].includes(result.status),`submission failed: ${JSON.stringify(result)}`);
    await delay(result.retryMs+Math.random()*200);
  }
  assert.fail(`submission did not recover: ${clientId}`);
}
async function batch(start,count,workers=8) {
  let next=start;
  await Promise.all(Array.from({length:workers},async()=>{
    while(next<start+count) { const index=next++; await submit(index); }
  }));
}
function verifyRetained(expected) {
  const counts=sql(`SELECT count(*),count(DISTINCT o.client_order_id),count(e.id) FROM orders o LEFT JOIN outbox_events e ON e.aggregate_id=o.id::text AND e.event_type='ORDER_SEND' WHERE o.client_order_id LIKE '${prefix}-%';`);
  assert.equal(counts,`${expected}|${expected}|${expected}`,'accepted orders/queue entries lost or duplicated');
}
async function waitFilled(expected) {
  const deadline=Date.now()+90000;
  while(Date.now()<deadline) {
    const counts=sql(`SELECT count(*),count(*) FILTER(WHERE status='FILLED') FROM orders WHERE client_order_id LIKE '${prefix}-%';`);
    if(counts===`${expected}|${expected}`) return;
    await delay(1000);
  }
  assert.fail('orders did not finish after recovery');
}
async function main() {
  const config=JSON.parse(command([...compose,'config','--format','json']));
  assert.equal(config.name,'fix-gateway-local');
  assert.equal(config.networks.default.internal,true);
  assert.equal(config.services.gateway.environment.ADAPTER_HOST_OVERRIDE,'fixsimulator');
  assert.deepEqual(Object.keys(config.services.gateway.networks),['default']);
  const raw=command([...compose,'ps','--format','json','access']).trim();
  const containers=raw.startsWith('[')?JSON.parse(raw):raw.split(/\r?\n/).map(JSON.parse);
  assert.ok(containers.some(c=>c.Project==='fix-gateway-local' && c.State==='running' &&
    c.Publishers.some(p=>p.PublishedPort===28080 && p.TargetPort===8080)));
  await expect('/ready');
  admin=(await expect('/api/v1/admin/login','','POST',{email:'admin@localhost',password:'admin123'})).token;
  const adapters=(await expect('/api/v1/admin/adapters',admin)).adapters.filter(a=>a.enabled);
  assert.ok(adapters.length===2 && adapters.every(a=>a.host==='fixsimulator' && a.type==='simulator'));
  const {broker}=await expect('/api/v1/admin/brokers',admin,'POST',{code:prefix,name:'Isolated reliability test'},201);
  const created=await expect(`/api/v1/admin/brokers/${broker.id}/tokens`,admin,'POST',{name:'Reliability',quik_firm:'SIM',expires_in_days:30,permissions:['orders:create','orders:read']},201);
  token=created.token; tokenId=created.id;
  try {
    const invalid=await call('/api/v1/orders',token,'POST',{...payload,symbol:'NO_SUCH_RELIABILITY_INSTRUMENT',client_order_id:`${prefix}-invalid`});
    assert.equal(invalid.status,400,'validation errors must remain permanent 400 responses');
    await batch(0,100);
    await waitFilled(100); verifyRetained(100);
    console.log('PASS 100 concurrent BUY/SELL submissions; 100 unique orders and durable queue entries');

    const until=Date.now()+7000;
    const statuses={};
    const flood=Promise.all(Array.from({length:32},async()=>{
      while(Date.now()<until) {
        try { const r=await call('/api/v1/orders/VENUE-invalid',token); statuses[r.status]=(statuses[r.status]||0)+1; }
        catch { statuses.network=(statuses.network||0)+1; }
      }
    }));
    const health=Promise.all(Array.from({length:5},async()=>{ await delay(200); const r=await call('/health'); assert.equal(r.status,200); assert.ok(r.ms<5000); }));
    await Promise.all([batch(100,30),flood,health]);
    await waitFilled(130); verifyRetained(130);
    assert.ok(statuses[429]>0,'flood did not exercise limiter');
    console.log(`PASS poll flood ${JSON.stringify(statuses)}; 30 trading requests preserved, health responsive`);

    command([...compose,'stop','fixsimulator']); simulatorStopped=true;
    await delay(1500);
    await batch(130,12,4);
    verifyRetained(142);
    const queued=Number(sql(`SELECT count(*) FROM outbox_events e JOIN orders o ON o.id::text=e.aggregate_id WHERE o.client_order_id LIKE '${prefix}-%' AND e.status IN ('PENDING','PROCESSING');`));
    assert.ok(queued>=12,'FIX outage did not leave trading requests queued');
    command([...compose,'kill','-s','SIGKILL','gateway']);
    command(['start','fix-gateway-local-gateway-1']);
    await waitHealth(); verifyRetained(142);
    console.log('PASS FIX outage + abrupt gateway termination: all 142 orders/queue entries retained');
    command(['start','fix-gateway-local-fixsimulator-1']); simulatorStopped=false;
    await waitFilled(142); verifyRetained(142);
    const trades=sql(`SELECT count(*),count(DISTINCT t.order_id) FROM executions t JOIN orders o ON o.id=t.order_id WHERE o.client_order_id LIKE '${prefix}-%' AND t.last_qty>0;`);
    assert.equal(trades,'142|142','missing or duplicate fills after recovery');
    command(['stop','fix-gateway-local-postgres-1']); databaseStopped=true;
    assert.equal((await call('/health')).status,200,'liveness must survive DB outage');
    const failed=await call('/api/v1/orders',token,'POST',{...payload,client_order_id:`${prefix}-142`});
    assert.equal(failed.status,503,'DB outage must be retryable, not invalid credentials or accepted');
    command(['start','fix-gateway-local-postgres-1']); databaseStopped=false;
    await delay(3000);
    verifyRetained(142);
    await submit(142); await waitFilled(143); verifyRetained(143);
    assert.equal(sql(`SELECT count(*),count(DISTINCT t.order_id) FROM executions t JOIN orders o ON o.id=t.order_id WHERE o.client_order_id LIKE '${prefix}-%' AND t.last_qty>0;`),'143|143','duplicate or missing fill after database recovery');
    console.log('PASS database outage: explicit 503, liveness responsive, retained queue, same-key retry succeeds');
    timings.sort((a,b)=>a-b);
    console.log(`PASS recovery: 143 orders preserved; accepted HTTP p95=${Math.round(timings[Math.floor(timings.length*.95)])}ms max=${Math.round(timings.at(-1))}ms`);
  } finally {
    if(simulatorStopped) command(['start','fix-gateway-local-fixsimulator-1']);
    if(databaseStopped) command(['start','fix-gateway-local-postgres-1']);
    await waitHealth();
    if(tokenId) await expect(`/api/v1/admin/tokens/${tokenId}/revoke`,admin,'POST');
  }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
