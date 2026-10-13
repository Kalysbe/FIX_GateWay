const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

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

function harness(rows) {
  const states = []; let index = 0;
  const icons = new Proxy({}, { get: () => () => null });
  const { DataTable } = load('components/DataTable.tsx', {
    react: { ...React, useId: () => 'search', useEffect: () => {}, useMemo: fn => fn(), useState: initial => { const i = index++; if (!(i in states)) states[i] = initial; return [states[i], value => states[i] = typeof value === 'function' ? value(states[i]) : value]; } },
    'react/jsx-runtime': require('react/jsx-runtime'), '../lib/table': table, './icons': { Icons: icons },
    './feedback': { EmptyState: ({ title }) => React.createElement('p', null, title), TableSkeleton: () => null },
  });
  const props = { title: 'Orders', rows, columns: [{ id: 'symbol', header: 'Инструмент', value: r => r.symbol }, { id: 'price', header: 'Цена', value: r => r.price, numeric: true }], rowKey: r => r.id, summary: list => [{ label: 'Сумма', value: list.reduce((n, r) => n + r.price, 0) }] };
  const render = () => { index = 0; return DataTable(props); };
  function find(node, predicate) { const matches = []; function walk(n) { if (!n || typeof n !== 'object') return; if (Array.isArray(n)) return n.forEach(walk); if (predicate(n)) matches.push(n); walk(n.props?.children); } walk(node); return matches; }
  return { render, find, props };
}
test('table totals use every filtered row, independent of the current page', () => {
  const h = harness(Array.from({ length: 25 }, (_, id) => ({ id, symbol: id < 23 ? 'AAA' : 'BBB', price: 2 })));
  let tree = h.render(); let html = renderToStaticMarkup(tree);
  assert.match(html, /Сумма<strong>50/); assert.equal(h.find(tree, n => n.type === 'tbody')[0].props.children[0].length, 20);
  h.find(tree, n => n.type === 'input' && n.props.type === 'search')[0].props.onChange({ target: { value: 'AAA' } });
  html = renderToStaticMarkup(h.render()); assert.match(html, /Сумма<strong>46/); assert.match(html, /из 23/);
});
test('sorting is accessible and filters reset pagination after navigation', () => {
  const h = harness(Array.from({ length: 25 }, (_, id) => ({ id, symbol: 'AAA', price: id })));
  h.find(h.render(), n => n.props?.['aria-label'] === 'Следующая страница')[0].props.onClick();
  assert.match(renderToStaticMarkup(h.render()), /21–25/);
  h.find(h.render(), n => n.type === 'button' && n.props.className === 'table-sort')[1].props.onClick();
  assert.match(renderToStaticMarkup(h.render()), /aria-sort="ascending"/);
  assert.match(renderToStaticMarkup(h.render()), /1–20/);
  h.find(h.render(), n => n.type === 'input' && n.props.type === 'search')[0].props.onChange({ target: { value: 'not-found' } });
  assert.match(renderToStaticMarkup(h.render()), /Ничего не найдено/);
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
