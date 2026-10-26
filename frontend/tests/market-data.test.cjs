const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

function load(file, imports = {}) {
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src', file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  const context = { exports: {}, JSON, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const raw = load('lib/rawOrders.ts');

function renderPage(text, error = null) {
  const rows = raw.parseDBJSON(text);
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: () => ({ data: error ? undefined : rows, error, isLoading: false, isFetching: false, refetch() {} }) },
    '../api': { apiWithHeaders: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 2000 },
    '../lib/rawOrders': raw,
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true, subscribe: () => () => {} }) },
    '../components/PageHeader': { PageHeader: ({ title }) => React.createElement('header', null, title) },
    '../components/feedback': { ErrorBanner: () => React.createElement('p', null, 'error-banner') },
    '../components/DataTable': { DataTable: ({ rows, columns, filters, emptyTitle }) => React.createElement('section', null, filters,
      React.createElement('div', null, columns.map(c => React.createElement('b', { key: c.id }, c.header))),
      rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, c.cell(r))))) : emptyTitle) },
  };
  return renderToStaticMarkup(React.createElement(load('pages/MarketDataPage.tsx', imports).default));
}

test('Params rows render with database column names and unrounded values', () => {
  const html = renderPage('[{"ClassCode":"LIST_GBA","SecCode":"GBA02280810","Last":123.4567890123456789,"Bid":null,"NumTrades":9007199254740993},{"ClassCode":"TQBR","SecCode":"SBER","Last":1,"Bid":1,"NumTrades":1}]');
  for (const column of ['ClassCode', 'SecCode', 'Last', 'Bid', 'NumTrades']) assert.match(html, new RegExp(`<b>${column}</b>`));
  assert.match(html, />123\.4567890123456789</);
  assert.match(html, />9007199254740993</);
  assert.match(html, />null</);
  assert.match(html, /<option value="LIST_GBA">LIST_GBA<\/option><option value="TQBR">TQBR<\/option>/);
  assert.doesNotMatch(html, /Подписка|FIX|Стакан/);
});

test('an unavailable source shows an error, not stale or FIX data', () => {
  const html = renderPage('[]', new Error('market data unavailable'));
  assert.match(html, /error-banner/);
  assert.match(html, /Таблица Params недоступна/);
});
