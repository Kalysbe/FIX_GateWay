const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src/lib/rawOrders.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const context = { exports: {} };
vm.runInNewContext(source, context);
const { parseDBJSON, dbValue, dbSortValue, orderKey, orderRowKey, nextOrderOffset } = context.exports;

test('rows without a full Orders key get no key collision and no link', () => {
  const a = { OrderNum: '1', ClassCode: null, TradeDate: '2026-10-22T00:00:00' };
  const b = { OrderNum: '2', ClassCode: null, TradeDate: '2026-10-22T00:00:00' };
  assert.equal(orderKey(a), '');
  assert.notEqual(orderRowKey(a), orderRowKey(b));
  assert.equal(orderKey({ OrderNum: '1', ClassCode: 'X' }), '');
});

test('sort values compare numbers numerically without changing the displayed text', () => {
  assert.equal(dbSortValue('120.00000000'), 120);
  assert.ok(dbSortValue('99.5') < dbSortValue('120'));
  assert.equal(dbSortValue('-1.25'), -1.25);
  assert.equal(dbSortValue('9007199254740993123'), '9007199254740993123', 'long integers keep exact text');
  assert.equal(dbSortValue('Активна'), 'Активна');
  assert.equal(dbSortValue(null), null);
  assert.equal(dbValue('120.00000000'), '120.00000000');
});

test('paging follows server headers only', () => {
  const h = (more, next) => ({ get: k => (k === 'X-Has-More' ? more : next) });
  assert.equal(nextOrderOffset(h('true', '200')), 200);
  assert.equal(nextOrderOffset(h('false', '200')), null);
  assert.equal(nextOrderOffset(h(null, null)), null);
  assert.equal(nextOrderOffset(h('true', 'x')), null);
});

test('database number tokens, nulls, escaped strings and unknown fields keep their source values', () => {
  const row = parseDBJSON(String.raw`{"OrderNum":9007199254740993123,"Price":120.00000000,"Balance":null,"FirmId":"2040","ClassCode":"LIST_GBA","TradeDate":"2026-10-22T00:00:00","Operation":"Купля","State":"Активна","Unknown":" value 123 \"456\" ","Flag":true,"Negative":-1.00000001,"Exponent":1e+20}`);
  assert.equal(row.OrderNum, '9007199254740993123');
  assert.equal(row.Price, '120.00000000');
  assert.equal(row.Balance, null);
  assert.equal(row.Flag, true);
  assert.equal(row.Negative, '-1.00000001');
  assert.equal(row.Exponent, '1e+20');
  assert.equal(row.Unknown, ' value 123 "456" ');
  assert.equal(row.Operation, 'Купля');
  assert.equal(row.State, 'Активна');
  assert.equal(Object.hasOwn(row, 'id'), false);
  assert.equal(dbValue(row.Balance), 'null');
  assert.equal(orderKey(row), 'quik-20261022-LIST_GBA-9007199254740993123');
});
