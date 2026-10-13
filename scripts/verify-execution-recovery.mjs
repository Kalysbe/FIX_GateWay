import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {setTimeout as delay} from 'node:timers/promises';

// Runs only against the disposable verification project. Never parameterize this
// fault test with a production database or compose project.
const compose=['compose','-p','fix-gateway-verification','-f','deploy/docker-compose.local.yml','-f','deploy/docker-compose.verification.yml'];
const command=args=>execFileSync(process.env.DOCKER_BIN||'docker',[...compose,...args],{encoding:'utf8',timeout:60000});
const config=JSON.parse(command(['config','--format','json']));
assert.equal(config.name,'fix-gateway-verification');
assert.equal(config.networks.default.internal,true);
assert.equal(config.services.gateway.environment.ADAPTER_HOST_OVERRIDE,'fixsimulator');
assert.equal(config.services.fixsimulator.environment.SIM_FILL_DELAY,'30s');
async function call(path,token='',method='GET',body,expected=200){
 const r=await fetch('http://127.0.0.1:38080'+path,{method,headers:{...(token?{Authorization:`Bearer ${token}`} : {}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(5000)});
 const result=await r.json();assert.equal(r.status,expected,`${method} ${path}: ${JSON.stringify(result)}`);return result;
}
async function until(fn,timeout=60000){const end=Date.now()+timeout;while(Date.now()<end){try{if(await fn())return;}catch{}await delay(300);}throw Error('Recovery timed out');}
const {token:admin}=await call('/api/v1/admin/login','','POST',{email:'admin@localhost',password:'admin123'});
const {broker}=await call('/api/v1/admin/brokers',admin,'POST',{code:`RECOVERY-${randomUUID()}`,name:'Execution journal recovery test'},201);
await call(`/api/v1/admin/brokers/${broker.id}/accounts`,admin,'POST',{code:'RECOVERY',name:'Test',quik_firm:'SIM',trading_account:'RECOVERY'},201);
const {token}=await call(`/api/v1/admin/brokers/${broker.id}/tokens`,admin,'POST',{name:'Recovery',quik_firm:'SIM',expires_in_days:30},201);
for(const restart of [false,true]){
 const {order}=await call('/api/v1/orders',token,'POST',{account_id:'RECOVERY',client_code:'RECOVERY',client_order_id:randomUUID(),symbol:'SBER',board:'TQBR',side:'BUY',order_type:'LIMIT',quantity:'1',price:'100'},201);
 await until(async()=>(await call(`/api/v1/orders/${order.id}`,token)).order.status==='ACKNOWLEDGED');
 let stopped=false;
 try{
  command(['stop','postgres']);stopped=true;
  await delay(35000);
  const files=command(['exec','-T','gateway','sh','-ec','find /app/fix-sessions/inbox -name "*.json" -type f']);
  assert.ok(files.includes('.json'),'FIX fill must be retained on disk during DB outage');
  const ready=await call('/ready','','GET',undefined,503);assert.equal(ready.execution_inbox_healthy,false);
  if(restart){command(['kill','-s','SIGKILL','gateway']);}
 }finally{if(stopped)command(['start','postgres']);}
 if(restart)command(['up','-d','gateway']);
 await until(async()=>{await call('/ready');return true;});
 const result=await call(`/api/v1/orders/${order.id}`,token);
 assert.equal(result.order.status,'FILLED');assert.equal(Number(result.order.filled_quantity),1);assert.equal(result.trades.length,1);
 command(['restart','gateway']);await until(async()=>{await call('/ready');return true;});
 assert.equal((await call(`/api/v1/orders/${order.id}`,token)).trades.length,1,'restart must not duplicate the fill');
 console.log(`PASS: execution survived DB outage${restart?' and SIGKILL':''}; exactly one trade after restart.`);
}
