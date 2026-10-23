const assert=require('node:assert/strict');
const {test}=require('node:test');
const {readFileSync}=require('node:fs');
const {resolve}=require('node:path');
const vm=require('node:vm');
const ts=require('typescript');
const React=require('react');

// Render ExecutionInbox with fixed hook state; mutations are captured, never sent.
function harness({role='OPERATOR',data}={}){
 const state=[];let index=0;const mutations=[];
 const source=ts.transpileModule(readFileSync(resolve(__dirname,'../src/components/ExecutionInbox.tsx'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX}}).outputText;
 const ctx={exports:{},Error,encodeURIComponent,Math,require:id=>{
  if(id==='react')return {...React,useState:initial=>{const n=index++;if(!(n in state))state[n]=initial;return [state[n],v=>{state[n]=typeof v==='function'?v(state[n]):v}];}};
  if(id==='react/jsx-runtime')return require(id);
  if(id==='@tanstack/react-query')return {useQuery:()=>({data,isLoading:false,error:null,refetch:async()=>{}}),useMutation:o=>({isPending:false,error:null,reset(){},mutate:v=>mutations.push({fn:o.mutationFn,v})})};
  if(id==='../api')return {getSessionUser:()=>({role}),api:async()=>({})};
  if(id==='./feedback')return {ErrorBanner:()=>null};
  if(id==='../labels')return {formatDateTime:v=>v};
  throw Error(id);
 }};
 vm.runInNewContext(source,ctx);
 function render(){index=0;return ctx.exports.ExecutionInbox({});}
 function nodes(tree,predicate){const out=[];function visit(n){if(!n||typeof n!=='object')return;if(Array.isArray(n)){n.forEach(visit);return;}if(predicate(n))out.push(n);visit(n.props?.children);}visit(tree);return out;}
 return {render,nodes,mutations};
}
const blocked={status:{pending:2,bytes:10,failed:true,healthy:false,oldest_age_seconds:40},
 blocked:{entry_id:'11111111-1111-1111-1111-111111111111',entry_name:'1_x.json',error:'execution correction requires venue reconciliation',failed_at:'t',attempts:3,read_error:false,received_at:'t',
  report:{adapter_id:'quik-fix-1',cl_ord_id:'C1',exec_id:'E1',exec_type:'H',ord_status:'2',exec_trans_type:'0',raw_fix:'35=8|150=H',protocol_reject:false,cancel_reject:false}},cases:[]};

test('quarantine requires a reason and targets the blocked entry',()=>{
 const h=harness({data:blocked});
 let tree=h.render();
 const button=h.nodes(tree,n=>n.type==='button'&&n.props.children==='Отложить в карантин')[0];
 assert.equal(button.props.disabled,true);
 h.nodes(tree,n=>n.type==='input')[0].props.onChange({target:{value:'  ARQA bust  '}});
 tree=h.render();
 h.nodes(tree,n=>n.type==='form')[0].props.onSubmit({preventDefault(){}});
 assert.deepEqual(JSON.parse(JSON.stringify(h.mutations[0].v)),{id:blocked.blocked.entry_id,reason:'ARQA bust'});
});

test('read-only role sees the blocked report but cannot quarantine',()=>{
 const h=harness({role:'AUDITOR',data:blocked});
 const tree=h.render();
 assert.equal(h.nodes(tree,n=>n.type==='form').length,0);
 assert.ok(h.nodes(tree,n=>n.type==='pre').length===1);
});

test('open case can be resolved with a note',()=>{
 const data={status:{pending:0,bytes:0,failed:false,healthy:true,oldest_age_seconds:0},blocked:null,
  cases:[{entry_id:'c1',entry_name:'n',adapter_id:'a',cl_ord_id:'C1',exec_id:'E1',exec_type:'H',order_id:null,apply_error:'e',attempts:1,reason:'r',quarantined_by:'u',quarantined_at:'t',resolved_by:null,resolved_at:null,resolution_note:null}]};
 const h=harness({data});
 let tree=h.render();
 h.nodes(tree,n=>n.type==='input')[0].props.onChange({target:{value:'Сделка отменена в QUIK'}});
 tree=h.render();
 h.nodes(tree,n=>n.type==='button'&&n.props.children==='Закрыть случай')[0].props.onClick();
 assert.deepEqual(JSON.parse(JSON.stringify(h.mutations[0].v)),{id:'c1',note:'Сделка отменена в QUIK'});
});
