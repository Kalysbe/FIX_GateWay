const {test}=globalThis;
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const ts=require('typescript');
function setup() {
  let now=0, next=0;
  const timers=new Map(), calls=[];
  const context={exports:{},setTimeout:(fn,ms)=>{const id=++next;timers.set(id,{fn,time:now+ms});return id;},clearTimeout:id=>timers.delete(id)};
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(require.resolve('../src/lib/eventRefresh.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,context);
  const refresh=context.exports.eventRefresh(keys=>calls.push(JSON.parse(JSON.stringify(keys))));
  return {module:context.exports,refresh,calls,timers,tick(ms){now+=ms;for(const [id,t] of [...timers])if(t.time<=now){timers.delete(id);t.fn();}}};
}
test('continuous event flood refreshes each key once per fixed window',()=>{
  const s=setup();
  for(let i=0;i<2000;i++){s.refresh.add([['notifications'],['admin-orders']]);s.tick(1);}
  assert.deepEqual(s.calls,[[['notifications'],['admin-orders']]]);
  assert.equal(s.timers.size,0);
  s.refresh.add([['admin-trades']]);s.tick(2000);
  assert.deepEqual(s.calls[1],[['admin-trades']]);
});
test('each stream event refreshes the pages that show its data',()=>{
  const {refreshKeysFor}=setup().module;
  const keys=type=>JSON.parse(JSON.stringify(refreshKeysFor(type))).map(k=>k.join('/'));
  // Quotes and an instrument's order book are Orders rows: an order event refreshes them too.
  for(const k of ['admin-orders','admin-quotes','admin-market-data/details','admin-trades','dashboard'])assert.ok(keys('order.updated').includes(k),k);
  assert.deepEqual(keys('trade.created'),keys('order.updated'));
  assert.deepEqual(keys('marketdata.tick'),['admin-market-data']);
  assert.deepEqual(keys('moneylimits.updated'),['admin-positions']);
  assert.deepEqual(keys('heartbeat'),[]);
});
test('unmount cancels pending refresh work',()=>{
  const s=setup();s.refresh.add([['notifications']]);s.refresh.stop();s.tick(5000);
  assert.equal(s.calls.length,0);assert.equal(s.timers.size,0);
});
