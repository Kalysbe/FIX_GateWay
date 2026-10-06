const assert=require('node:assert/strict');
const {test}=require('node:test');
const {readFileSync}=require('node:fs');
const {resolve}=require('node:path');
const vm=require('node:vm');
const ts=require('typescript');
const React=require('react');

// Run component handlers with deterministic hook state; never perform network I/O.
function harness(file,name,{role='ADMIN',query={},failure}={}){
 const state=[];let index=0;const requests=[],invalidations=[],queries=[];
 const source=ts.transpileModule(readFileSync(resolve(__dirname,'../src/components',file),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX}}).outputText;
 const ctx={exports:{},Error,require:id=>{
  if(id==='react')return {...React,useState:initial=>{const n=index++;if(!(n in state))state[n]=initial;return [state[n],v=>{state[n]=typeof v==='function'?v(state[n]):v}];}};
  if(id==='react/jsx-runtime')return require(id);
  if(id==='@tanstack/react-query')return {useQuery:options=>{queries.push(options);return {refetch:async()=>{},...query}},useQueryClient:()=>({invalidateQueries:async options=>invalidations.push(options)}),useMutation:()=>({isPending:false,mutate:v=>requests.push(v)})};
  if(id==='../api')return {getSessionUser:()=>({role}),api:async(path,init)=>{requests.push({path,...init});if(failure)throw failure;return {};}};
  if(id==='./feedback')return {ErrorBanner:()=>null};
  if(id==='./TradingLimitHistory')return {TradingLimitHistory:()=>null};
  if(id==='../labels')return {formatDateTime:v=>v};
  if(id==='react-router-dom')return {Link:()=>null};
  throw Error(id);
 }};
 vm.runInNewContext(source,ctx);
 function render(){index=0;return ctx.exports[name]({});}
 function nodes(tree,predicate){const out=[];function visit(n){if(!n||typeof n!=='object')return;if(Array.isArray(n)){n.forEach(visit);return;}if(predicate(n))out.push(n);visit(n.props?.children);}visit(tree);return out;}
 return {render,nodes,requests,invalidations,queries};
}
const policy={broker_id:0,paused:true,max_quantity:'10',max_open_orders:3,revision:2,updated_at:'2026-10-05T00:00:00Z'};
test('saving policy sends only writable fields and refreshes history',async()=>{
 const h=harness('TradingLimits.tsx','TradingLimits',{query:{data:{policies:[policy]}}});
 let tree=h.render();h.nodes(tree,n=>n.type==='button'&&n.props.children==='Изменить')[0].props.onClick();
 tree=h.render();const inputs=h.nodes(tree,n=>n.type==='input');inputs.find(n=>n.props.minLength===3).props.onChange({target:{value:'Operator test'}});
 tree=h.render();await h.nodes(tree,n=>n.type==='form')[0].props.onSubmit({preventDefault(){}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(JSON.parse(h.requests[0].body),{broker_id:0,paused:true,revision:2,max_quantity:'10',max_open_orders:3,reason:'Operator test'});
 assert.equal(h.invalidations[0].queryKey[0],'trading-limit-history');
 assert.equal(h.nodes(h.render(),n=>n.type==='form').length,0);
});
test('revision conflict keeps edits visible and displays API error',async()=>{
 const h=harness('TradingLimits.tsx','TradingLimits',{query:{data:{policies:[policy]}},failure:Error('policy changed; reload')});
 h.nodes(h.render(),n=>n.type==='button'&&n.props.children==='Изменить')[0].props.onClick();
 await h.nodes(h.render(),n=>n.type==='form')[0].props.onSubmit({preventDefault(){}});
 await new Promise(resolve=>setImmediate(resolve));
 const tree=h.render();assert.equal(h.nodes(tree,n=>n.type==='form').length,1);
 assert.equal(h.nodes(tree,n=>n.props?.role==='status')[0].props.children,'policy changed; reload');
});
test('read-only cannot edit policies or fetch audit history',()=>{
 const h=harness('TradingLimits.tsx','TradingLimits',{role:'READ_ONLY',query:{data:{policies:[policy]}}});
 assert.equal(h.nodes(h.render(),n=>n.type==='button'||n.type==='form').length,0);
 const history=harness('TradingLimitHistory.tsx','TradingLimitHistory',{role:'READ_ONLY'});
 assert.equal(history.render(),null);assert.equal(history.queries[0].enabled,false);
});
test('history uses stable cursor and resets it when filter changes',()=>{
 const h=harness('TradingLimitHistory.tsx','TradingLimitHistory',{role:'AUDITOR',query:{data:{history:[],next_before_id:21}}});
 h.nodes(h.render(),n=>n.type==='button'&&n.props.children==='Более ранние изменения')[0].props.onClick();
 let tree=h.render();assert.equal(h.queries.at(-1).queryKey[2],21);
 h.nodes(tree,n=>n.type==='input')[0].props.onChange({target:{value:'0'}});
 h.render();assert.equal(h.queries.at(-1).queryKey[1],'0');assert.equal(h.queries.at(-1).queryKey[2],null);
});
test('unconfirmed reconciliation case has no close action',()=>{
 const h=harness('ReconciliationQueue.tsx','ReconciliationQueue',{query:{data:{cases:[{event_id:1,order_id:'id',client_order_id:'client',event_type:'ORDER_SEND',order_status:'SENT',last_error:'unknown',evidence_event_id:null,resolved_at:null}]}}});
 assert.equal(h.nodes(h.render(),n=>n.type==='button').length,0);
});
test('confirmed case requires a note and sends the selected event id',()=>{
 const h=harness('ReconciliationQueue.tsx','ReconciliationQueue',{query:{data:{cases:[{event_id:8,order_id:'id',client_order_id:'client',event_type:'ORDER_SEND',order_status:'ACKNOWLEDGED',last_error:'unknown',evidence_event_id:9,resolved_at:null}]}}});
 let tree=h.render();assert.equal(h.nodes(tree,n=>n.type==='button')[0].props.disabled,true);
 h.nodes(tree,n=>n.type==='input')[0].props.onChange({target:{value:'Verified venue report'}});
 tree=h.render();const button=h.nodes(tree,n=>n.type==='button')[0];assert.equal(button.props.disabled,false);button.props.onClick();
 assert.equal(h.requests[0].id,8);assert.equal(h.requests[0].note,'Verified venue report');
});
test('auditor cannot close even a confirmed reconciliation case',()=>{
 const h=harness('ReconciliationQueue.tsx','ReconciliationQueue',{role:'AUDITOR',query:{data:{cases:[{event_id:8,order_id:'id',client_order_id:'client',event_type:'ORDER_SEND',order_status:'ACKNOWLEDGED',last_error:'unknown',evidence_event_id:9,resolved_at:null}]}}});
 assert.equal(h.nodes(h.render(),n=>n.type==='button'||n.type==='input').length,0);
});
