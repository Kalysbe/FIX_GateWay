const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

function load(file, imports) {
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src', file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  const context = { exports: {}, JSON, Map, Set, Array, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const raw = load('lib/rawOrders.ts', {});
function render(rows, value, error = null) {
  const { FirmSelect } = load('components/FirmSelect.tsx', {
    react: React, 'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: () => ({ data: rows, isLoading: false, error }) },
    '../api': { apiWithHeaders: () => { throw Error('unexpected network request'); } },
    '../lib/rawOrders': raw,
  });
  return renderToStaticMarkup(React.createElement(FirmSelect, { value, onChange() {} }));
}

test('firms are offered from QUIK Firms rows', () => {
  const html = render(raw.parseDBJSON('[{"Exchange":"KSE","FirmId":"2040  ","FirmName":"Сенти"},{"Exchange":"KSE","FirmId":"9999","FirmName":null}]'), '2040');
  assert.match(html, /<option value="2040" selected="">2040 · Сенти<\/option>/);
  assert.match(html, /<option value="9999">9999<\/option>/);
  assert.doesNotMatch(html, /нет в QUIK Firms/);
});

test('a token firm missing from QUIK is shown as such, and an outage disables the choice', () => {
  assert.match(render([], 'OLD'), /OLD \(нет в QUIK Firms\)/);
  const html = render(undefined, '', new Error('QUIK accounts and firms source unavailable'));
  assert.match(html, /disabled=""/);
  assert.match(html, /Таблица QUIK Firms недоступна/);
});
