const assert=require('node:assert/strict');
const {test}=globalThis;
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
// TradingLimits, ReconciliationQueue: tests/operations-forms.test.tsx (Testing Library).
test('read-only cannot fetch audit history',()=>{
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
