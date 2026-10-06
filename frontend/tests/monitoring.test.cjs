const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const compiled = ts.transpileModule(readFileSync(resolve(__dirname,'../src/pages/MonitoringPage.tsx'),'utf8'), {
  compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX},
}).outputText;

function render(monitoring) {
  const queries=[];
  const context={exports:{},AbortSignal,require:(name)=>{
    if(name==='react' || name==='react/jsx-runtime') return require(name);
    if(name==='@tanstack/react-query') return {useQuery:options=>{
      queries.push(options);
      if(options.queryKey[0]==='monitoring') return monitoring;
      if(options.queryKey[0]==='monitoring-adapters') return {data:{adapters:[{id:'live',name:'Live FIX',status:'ONLINE',enabled:true}]}};
      return {isLoading:true,isFetching:false,data:undefined}; // day-wide query never finishes
    }};
    if(name==='react-router-dom') return {Link:({children,to})=>React.createElement('a',{href:to},children)};
    if(name==='../api') return {api:()=>{throw Error('unexpected network request')}};
    if(name==='../components/breadcrumbs') return {useBreadcrumbs:()=>{}};
    if(name==='../components/feedback') return {PageSpinner:()=>React.createElement('p',null,'Loading spinner'),ErrorBanner:({error})=>React.createElement('p',null,error?.message)};
    if(name==='../components/StatusBadge') return {StatusBadge:({status})=>React.createElement('span',null,status)};
    if(name==='../lib/adapters') return {displayAdapterName:name=>name,isHiddenAdapter:a=>a.enabled===false};
    if(name.endsWith('.css')) return {};
    throw Error(name);
  }};
  vm.runInNewContext(compiled,context);
  return {html:renderToStaticMarkup(React.createElement(context.exports.default)),queries};
}
test('live monitoring renders while daily dashboard remains unavailable',()=>{
  const {html,queries}=render({data:{timestamp:'2026-10-02T10:00:00Z',counters:{process_resident_memory_bytes:1048576,orders_queue_size:7},adapters:{},heap_bytes:100,goroutines:12,cpu_cores:4,database:{acquired:2,total:3,max:20}}});
  assert.match(html,/Состояние шлюза в реальном времени/);
  assert.match(html,/Live FIX/);
  assert.match(html,/Суточная статистика ещё не загружена/);
  assert.doesNotMatch(html,/Loading spinner/);
  assert.equal(queries.find(q=>q.queryKey[0]==='monitoring-daily').enabled,false);
});
test('failed telemetry shows retryable error rather than endless loading',()=>{
  const {html}=render({data:undefined,error:new Error('telemetry timed out'),isLoading:false});
  assert.match(html,/telemetry timed out/);
  assert.doesNotMatch(html,/Loading spinner/);
});
