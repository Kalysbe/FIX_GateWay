const assert=require('node:assert/strict');
const {test}=require('node:test');
const {readFileSync}=require('node:fs');
const {resolve}=require('node:path');
const vm=require('node:vm');
const ts=require('typescript');
const React=require('react');
const {renderToStaticMarkup}=require('react-dom/server');
const source=ts.transpileModule(readFileSync(resolve(__dirname,'../src/pages/GrpcPage.tsx'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX}}).outputText;
const broker={id:'b',broker_id:7,broker_name:'Test broker',broker_code:'TEST',role:'BROKER',transport:'gRPC',peer:'127.0.0.1:123',connected_at:'2026-10-06T10:00:00Z',topics:['order.updated'],paths:['/api/v1/orders']};
function harness(role='ADMIN',connections=[broker]){
 const state=[];let index=0,listener;const queries=[];
 const data={instance_id:'first',revision:1,connections};
 const ctx={exports:{},require:id=>{
  if(id==='react')return {...React,useState:initial=>{const n=index++;if(!(n in state))state[n]=initial;return [state[n],value=>state[n]=typeof value==='function'?value(state[n]):value];},useEffect:fn=>fn()};
  if(id==='react/jsx-runtime')return require(id);
  if(id==='@tanstack/react-query')return {useQuery:options=>{queries.push(options);return {data,refetch:()=>{}}}};
  if(id==='../api')return {getSessionUser:()=>({role}),api:()=>{throw Error('unexpected network')}};
  if(id==='../hooks/useAdminStream')return {useAdminStream:()=>({connected:true,subscribe:fn=>{listener=fn;return ()=>{}}})};
  if(id==='../components/breadcrumbs')return {useBreadcrumbs:()=>{}};
  if(id==='../components/PageHeader')return {PageHeader:({title})=>React.createElement('h1',null,title)};
  if(id==='../components/feedback')return {ErrorBanner:()=>null};
  if(id==='../components/DataTable')return {DataTable:({rows,columns})=>React.createElement('table',null,React.createElement('tbody',null,rows.map(row=>React.createElement('tr',{key:row.id},columns.map(c=>React.createElement('td',{key:c.id},c.cell ? c.cell(row) : c.value(row)))))))};
  if(id==='react-router-dom')return {Link:({to,children})=>React.createElement('a',{href:to},children)};
  throw Error(id);
 }};
 vm.runInNewContext(source,ctx);
 const render=()=>{index=0;return renderToStaticMarkup(ctx.exports.default())};
 return {render,queries,emit:snapshot=>listener({type:'subscriptions.updated',data:snapshot})};
}
test('gRPC page shows broker topics and removes disconnected clients live',()=>{
 const h=harness();h.render();assert.match(h.render(),/Test broker/);assert.match(h.render(),/\/api\/v1\/orders/);
 h.emit({instance_id:'first',revision:2,connections:[]});assert.doesNotMatch(h.render(),/Test broker/);
 // Delayed snapshot from before disconnect must not bring the client back.
 h.emit({instance_id:'first',revision:1,connections:[broker]});assert.doesNotMatch(h.render(),/Test broker/);
});
test('gRPC page accepts new process snapshot after sequence reset',()=>{
 const h=harness();h.render();h.render();
 h.emit({instance_id:'second',revision:0,connections:[]});assert.doesNotMatch(h.render(),/Test broker/);
});
test('read-only cannot fetch gRPC client identities',()=>{
 const h=harness('READ_ONLY');assert.match(h.render(),/недоступен/);assert.equal(h.queries[0].enabled,false);
});
test('native external client using admin JWT is shown while frontend bridges are hidden',()=>{
 const external={...broker,id:'external',broker_id:undefined,broker_name:undefined,email:'external-system@example.test',role:'SUPER_ADMIN'};
 const bridge={...broker,id:'bridge',broker_id:undefined,broker_name:undefined,email:'frontend@example.test',transport:'Browser → gRPC'};
 const h=harness('ADMIN',[external,bridge]);h.render();const html=h.render();
 assert.match(html,/external-system@example.test/);
 assert.doesNotMatch(html,/frontend@example.test/);
 assert.doesNotMatch(html,/Только брокеры/);
 assert.match(html,/Подключений внешних систем: 1/);
});
