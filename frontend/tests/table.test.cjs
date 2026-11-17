const assert = require('node:assert/strict');
const { test } = globalThis;
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');

function load(file, imports = {}) {
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src', file), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX } }).outputText;
  const context = { exports: {}, Intl, Date, Number, require: id => { if (id in imports) return imports[id]; throw Error(id); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const table = load('lib/table.ts');
test('decimal prices and chronological dates sort by their values without mutating live rows', () => {
  const rows = [{ p: '100', d: '2026-09-30' }, { p: '9.5', d: '2026-10-01' }, { p: '2', d: '2026-09-29' }, { p: null, d: '' }];
  assert.deepEqual(Array.from(table.sortRows(rows, r => table.numberValue(r.p), 'asc'), r => r.p), ['2', '9.5', '100', null]);
  assert.deepEqual(Array.from(table.sortRows(rows, r => table.numberValue(r.p), 'desc'), r => r.p), ['100', '9.5', '2', null]);
  assert.deepEqual(Array.from(table.sortRows(rows, r => table.dateValue(r.d), 'desc'), r => r.d), ['2026-10-01', '2026-09-30', '2026-09-29', '']);
  assert.equal(rows[0].p, '100');
});
test('search matches multiple terms, Cyrillic and ё across fields', () => {
  const rows = [{ name: 'Счёт Freedom', firm: '2040' }, { name: 'Freedom', firm: '3101' }];
  assert.equal(table.filterRows(rows, '  СЧЕТ  2040 ', r => r.name + ' ' + r.firm).length, 1);
  assert.equal(table.filterRows(rows, 'Freedom 2040', r => r.name + ' ' + r.firm)[0], rows[0]);
});
test('pagination clamps a shrinking live dataset and keeps paired executions together', () => {
  const deals = Array.from({ length: 21 }, (_, id) => ({ id, trades: [{ side: 'BUY' }, { side: 'SELL' }] }));
  const last = table.paginateRows(deals, 1, 20);
  assert.equal(last.rows.length, 1); assert.equal(last.rows[0].trades.length, 2); assert.equal(last.start, 21);
  assert.equal(table.paginateRows(deals.slice(0, 3), 1, 20).page, 0);
  assert.equal(table.paginateRows([], 99, 20).start, 0);
  assert.equal(table.paginateRows(deals, -1, 0).page, 0);
});
test('CSV escapes quotations, line breaks and spreadsheet formulas', () => {
  assert.equal(table.csvCell('firm "A"'), '"firm ""A"""');
  assert.equal(table.csvCell('=HYPERLINK("x")'), '"\'=HYPERLINK(""x"")"');
  assert.equal(table.csvCell('line\nnext'), '"line\nnext"');
  assert.equal(table.csvCell(-3.5), '"-3.5"');
});

const adapters = load('lib/adapters.ts');
const { connectionSummary } = load('lib/connections.ts', { './adapters': adapters });
test('QUIK status excludes disabled adapters and simulators and never invents a live connection', () => {
  const live = { id: 'quik-1', type: 'arqa', enabled: true, status: 'ONLINE' };
  assert.equal(connectionSummary([live]).state, 'online');
  assert.equal(connectionSummary([live, { ...live, id: 'quik-2', status: 'OFFLINE' }]).state, 'degraded');
  assert.equal(connectionSummary([{ ...live, status: 'DEGRADED' }]).state, 'degraded');
  assert.equal(connectionSummary([{ ...live, status: 'OFFLINE' }]).state, 'offline');
  assert.equal(connectionSummary([live], true).state, 'unknown');
  assert.equal(connectionSummary([{ ...live, enabled: false }, { ...live, type: 'simulator' }]).state, 'empty');
  assert.equal(connectionSummary([{ ...live, type: 'simulator' }]).simulators.length, 1);
});
