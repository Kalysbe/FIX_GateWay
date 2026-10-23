import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { setTimeout as sleep } from 'node:timers/promises';
import { mkdirSync, writeFileSync } from 'node:fs';

// Fixed endpoint + verified internal network: this tool cannot load-test UAT.
const profile=process.env.GATEWAY_TEST_PROFILE||'local';
assert.ok(['local','verification'].includes(profile));
const project=profile==='verification'?'fix-gateway-verification':'fix-gateway-local';
const base=profile==='verification'?'http://127.0.0.1:38080':'http://127.0.0.1:28080', docker=process.env.DOCKER_BIN||'docker';
const compose=profile==='verification'?['compose','-p',project,'-f','deploy/docker-compose.local.yml','-f','deploy/docker-compose.verification.yml']:['compose','-f','deploy/docker-compose.local.yml'];
const exec=promisify(execFile);
const run=randomUUID(), prefix=`CAPACITY-${run}`;
const seconds=Number(process.env.CAPACITY_SECONDS||20);
assert.ok(seconds>=10 && seconds<=120);
const rates=(process.env.CAPACITY_RATES||'10,25,50,100,200,400').split(',').map(Number);
assert.ok(rates.every(r=>r>0&&r<=1000));
const modes=(process.env.CAPACITY_MODES||'adapter-1,adapter-2,both').split(',');
assert.ok(modes.every(m=>['adapter-1','adapter-2','both'].includes(m)));
let admin,token,tokenId,brokerId;
const report={run,started:new Date().toISOString(),environment:base,seconds,simulatorFillDelaySeconds:3,results:[]};
async function request(path,auth=admin,method='GET',body){
  const t=performance.now();
  const r=await fetch(base+path,{method,headers:{...(auth?{Authorization:`Bearer ${auth}`} :{}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(15000)});
  const data=await r.json();return {status:r.status,data,ms:performance.now()-t};
}
async function expect(path,auth,method,body,status=200){const r=await request(path,auth,method,body);assert.equal(r.status,status,`${path}: ${JSON.stringify(r.data)}`);return r.data;}
async function sql(q){
  const child=exec(docker,[...compose,'exec','-T','postgres','psql','-U','gateway','-d','fix_gateway_local','-v','ON_ERROR_STOP=1','-Atc',q],{timeout:15000,maxBuffer:2*1024*1024});
  return (await child).stdout.trim();
}
async function online(id){
  const until=Date.now()+30000;
  while(Date.now()<until){const a=(await expect('/api/v1/admin/adapters')).adapters.find(a=>a.id===id);if(a?.status==='ONLINE')return;await sleep(500);}
  throw Error(`adapter ${id} did not become ONLINE`);
}
function save(){mkdirSync('artifacts/capacity',{recursive:true});writeFileSync(`artifacts/capacity/${run}.json`,JSON.stringify(report,null,2));}
async function stats(stage){return JSON.parse(await sql(`
WITH o AS (SELECT * FROM orders WHERE broker_id=${brokerId} AND client_order_id LIKE '${stage}-%'),
e AS (SELECT x.order_id,min(x.created_at) FILTER(WHERE x.last_qty>0) AS fill,count(*) FILTER(WHERE x.last_qty>0) AS fills FROM executions x JOIN o ON o.id=x.order_id GROUP BY x.order_id),
a AS (SELECT x.order_id,min(x.created_at) AS ack FROM order_events x JOIN o ON o.id=x.order_id WHERE x.to_status='ACKNOWLEDGED' GROUP BY x.order_id),
q AS (SELECT x.* FROM outbox_events x JOIN o ON o.id::text=x.aggregate_id WHERE x.event_type='ORDER_SEND')
SELECT json_build_object('orders',(SELECT count(*) FROM o),'filled',(SELECT count(*) FROM o WHERE status='FILLED'),
'ack',(SELECT count(*) FROM a),'fills',(SELECT coalesce(sum(fills),0) FROM e),
'done',(SELECT count(*) FROM q WHERE status='DONE'),'pending',(SELECT count(*) FROM q WHERE status IN ('PENDING','PROCESSING')),
'failed',(SELECT count(*) FROM q WHERE status='FAILED'),'outbox',(SELECT count(*) FROM q),
'oldest_seconds',(SELECT coalesce(max(extract(epoch from now()-created_at)),0) FROM q WHERE status IN ('PENDING','PROCESSING')),
'ack_p95_ms',(SELECT percentile_cont(.95) WITHIN GROUP(ORDER BY extract(epoch from a.ack-o.created_at)*1000) FROM o JOIN a ON o.id=a.order_id),
'fill_p95_ms',(SELECT percentile_cont(.95) WITHIN GROUP(ORDER BY extract(epoch from e.fill-o.created_at)*1000) FROM o JOIN e ON o.id=e.order_id WHERE e.fill IS NOT NULL),
'adapters',(SELECT coalesce(json_object_agg(coalesce(adapter_id,'UNROUTED'),n),'{}') FROM (SELECT adapter_id,count(*) n FROM o GROUP BY adapter_id) a));`));}
async function stage(mode,rate,duration=seconds,warmup=false){
  const id=`${prefix}-${mode}-${rate}${warmup?'-warmup':''}`, result={mode,offeredRps:rate,durationSeconds:duration,warmup,statuses:{},samples:[],skippedByGenerator:0};
  const latencies=[], accepted=new Set(), inFlight=new Set();
  const start=performance.now();let next=0,finished=false,samplingError;
  const sampling=(async()=>{while(!finished){await sleep(2000);if(finished)break;const s=await stats(id);result.samples.push({seconds:(performance.now()-start)/1000,...s});}})().catch(e=>{samplingError=e;});
  const submit=async n=>{try{
    const r=await request('/api/v1/orders',token,'POST',{account_id:'CAPACITY',client_code:'CAPACITY',symbol:'SBER',board:'TQBR',side:n%2?'SELL':'BUY',order_type:'LIMIT',time_in_force:'DAY',quantity:'1',price:'100',client_order_id:`${id}-${n}`});
    result.statuses[r.status]=(result.statuses[r.status]||0)+1;
    if(r.status===201||r.status===200){accepted.add(r.data.order.id);latencies.push(r.ms);}
    else assert.ok([429,503].includes(r.status),JSON.stringify(r.data));
  }catch(e){result.statuses.network=(result.statuses.network||0)+1;result.lastError=e.message;}};
  while(performance.now()-start<duration*1000){
    const due=Math.min(Math.floor((performance.now()-start)*rate/1000),duration*rate);
    while(next<due){const n=next++;if(inFlight.size>=128){result.skippedByGenerator++;continue;}const p=submit(n).finally(()=>inFlight.delete(p));inFlight.add(p);}
    await sleep(10);
  }
  await Promise.all(inFlight);finished=true;await sampling;if(samplingError)throw samplingError;
  result.attempted=next-result.skippedByGenerator;result.accepted=accepted.size;result.end=await stats(id);
  const samples=result.samples.filter(s=>s.seconds>=5);
  if(samples.length>=2){const first=samples[0],last=samples.at(-1),dt=last.seconds-first.seconds;result.dispatchRps=(last.done-first.done)/dt;result.ackRps=(last.ack-first.ack)/dt;result.fillRps=(last.filled-first.filled)/dt;result.queueGrowthRps=(last.pending-first.pending)/dt;}
  latencies.sort((a,b)=>a-b);result.httpP95Ms=latencies[Math.floor(latencies.length*.95)]||0;
  result.rejectionFree=!result.statuses.network && Object.keys(result.statuses).every(s=>s==='200'||s==='201');
  result.stable=result.accepted>=rate*duration*.95&&!result.statuses.network&&result.skippedByGenerator===0&&result.end.pending<=Math.max(3,rate*.5)&&Math.abs(result.queueGrowthRps||0)<=Math.max(2,rate*.05)&&(result.end.ack_p95_ms??Infinity)<1500&&result.end.failed===0;
  const drainStart=performance.now();let final=result.end;
  while(final.filled<final.orders||final.pending){assert.ok(performance.now()-drainStart<120000,'queue did not drain within 120 seconds');await sleep(1000);final=await stats(id);}
  result.drainSeconds=(performance.now()-drainStart)/1000;result.final=final;
  assert.equal(final.orders,result.accepted,'HTTP acceptance / persisted orders mismatch (inspect uncertain results)');
  assert.equal(final.ack,final.orders,'missing venue acknowledgment');
  assert.equal(final.outbox,final.orders,'durable queue row missing');assert.equal(final.fills,final.orders,'missing or duplicated fill');assert.equal(final.failed,0);
  report.results.push(result);save();
  console.log(JSON.stringify({mode,rate,accepted:result.accepted,statuses:result.statuses,stable:result.stable,dispatchRps:result.dispatchRps,ackRps:result.ackRps,ackP95:result.end.ack_p95_ms,pending:result.end.pending,drainSeconds:result.drainSeconds}));
  return result;
}
async function main(){
  const cfg=JSON.parse(execFileSync(docker,[...compose,'config','--format','json'],{encoding:'utf8'}));
  assert.equal(cfg.name,project);assert.equal(cfg.networks.default.internal,true);assert.equal(cfg.services.gateway.environment.ADAPTER_HOST_OVERRIDE,'fixsimulator');assert.deepEqual(Object.keys(cfg.services.gateway.networks),['default']);
  report.simulatorFillDelaySeconds=Number.parseFloat(cfg.services.fixsimulator.environment.SIM_FILL_DELAY);
  assert.equal(await sql('select current_database()'),'fix_gateway_local');
  admin=(await expect('/api/v1/admin/login','','POST',{email:'admin@localhost',password:'admin123'})).token;
  const adapters=(await expect('/api/v1/admin/adapters')).adapters.filter(a=>a.enabled);assert.equal(adapters.length,2);assert.ok(adapters.every(a=>a.type==='simulator'&&a.host==='fixsimulator'));
  const {broker}=await expect('/api/v1/admin/brokers',admin,'POST',{code:prefix,name:'Isolated adapter capacity benchmark'},201);brokerId=broker.id;
  await expect(`/api/v1/admin/brokers/${brokerId}`,admin,'PATCH',{rate_limit_rps:20000,rate_limit_orders_per_sec:20000});
  await expect(`/api/v1/admin/brokers/${brokerId}/accounts`,admin,'POST',{code:'CAPACITY',name:'Capacity test',quik_firm:'SIM',trading_account:'CAPACITY'},201);
  const t=await expect(`/api/v1/admin/brokers/${brokerId}/tokens`,admin,'POST',{name:'Capacity benchmark',quik_firm:'SIM',expires_in_days:30,permissions:['orders:create','orders:read']},201);token=t.token;tokenId=t.id;
  try{
    for(const mode of modes){
      // Enable the target before disabling the other: never leave zero adapters.
      if(mode==='both'){await expect('/api/v1/admin/adapters/adapter-1/enable',admin,'POST');await expect('/api/v1/admin/adapters/adapter-2/enable',admin,'POST');await online('adapter-1');await online('adapter-2');}
      else {const other=mode==='adapter-1'?'adapter-2':'adapter-1';await expect(`/api/v1/admin/adapters/${mode}/enable`,admin,'POST');await online(mode);await expect(`/api/v1/admin/adapters/${other}/disable`,admin,'POST');}
      await sleep(1000);
      if(process.env.CAPACITY_WARMUP==='1')await stage(mode,10,10,true);
      for(const rate of rates){const r=await stage(mode,rate);if(!r.stable)break;}
    }
  }finally{
    await expect('/api/v1/admin/adapters/adapter-1/enable',admin,'POST');await expect('/api/v1/admin/adapters/adapter-2/enable',admin,'POST');
    if(tokenId)await expect(`/api/v1/admin/tokens/${tokenId}/revoke`,admin,'POST');
    report.finished=new Date().toISOString();save();
  }
}
main().catch(e=>{report.error=e.stack;save();console.error(e.stack);process.exitCode=1;});
