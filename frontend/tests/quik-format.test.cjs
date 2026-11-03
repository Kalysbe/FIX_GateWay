const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src/lib/quikFormat.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const context = { exports: {} };
vm.runInNewContext(source, context);
const { formatNumber, formatDate, formatDateTime, formatTime, stateTone, sideClass } = context.exports;

test('numbers drop trailing zeros, group thousands and keep every digit', () => {
  assert.equal(formatNumber('6500.00000000'), '6 500');
  assert.equal(formatNumber('4.9900', 2), '4,99');
  assert.equal(formatNumber('95', 2), '95,00');
  assert.equal(formatNumber('0.00010000'), '0,0001');
  assert.equal(formatNumber('-1234.5'), '-1 234,5');
  assert.equal(formatNumber('9007199254740993123'), '9 007 199 254 740 993 123');
  assert.equal(formatNumber('1e+20'), '1e+20', 'unknown forms stay as they are');
});

test('dates are compact and midnight is a bare date', () => {
  assert.equal(formatDate('2027-09-15T00:00:00'), '15.09.2027');
  assert.equal(formatDateTime('2026-11-02T16:57:24'), '02.11.26 16:57:24');
  assert.equal(formatDateTime('2026-11-02T00:00:00'), '02.11.26');
  assert.equal(formatTime('1900-01-01 16:40:15'), '16:40:15');
  assert.equal(formatDate('soon'), 'soon');
});

test('states and sides get their tone', () => {
  assert.equal(stateTone('Исполнена'), 'badge-success');
  assert.equal(stateTone('Активна'), 'badge-info');
  assert.equal(stateTone('Снята'), 'badge-muted');
  assert.equal(sideClass('Купля'), 'side-buy');
  assert.equal(sideClass('Продажа'), 'side-sell');
});
