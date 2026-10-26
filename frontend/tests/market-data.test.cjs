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
    '../components/MarketClassesSettings': { MarketClassesSettings: () => React.createElement('aside', null, 'class-settings') },
    '../components/DataTable': { DataTable: ({ rows, columns, filters, emptyTitle }) => React.createElement('section', null, filters,
      React.createElement('div', null, columns.map(c => React.createElement('b', { key: c.id }, c.header))),
      rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, c.cell(r))))) : emptyTitle) },
  };
  return renderToStaticMarkup(React.createElement(load('pages/MarketDataPage.tsx', imports).default));
}

test('Params rows render with database column names and unrounded values', () => {
  const html = renderPage('[{"ClassCode":"LISTBND","SecCode":"GBA02270915","CouponValue":2.500000,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2","SecFullName":"ГКО-2 летние","ClassName":"Листинговые:облигации"},{"ClassCode":"TQBR","SecCode":"SBER","CouponValue":9007199254740993,"status":null,"FaceUnit":null,"SecShortName":null,"SecFullName":null,"ClassName":null}]');
  const columns = ['ClassCode', 'SecCode', 'CouponValue', 'status', 'FaceUnit', 'SecShortName', 'SecFullName', 'ClassName'];
  assert.match(html, new RegExp(columns.map(c => `<b>${c}</b>`).join('')));
  assert.match(html, />2\.500000</);
  assert.match(html, />9007199254740993</);
  assert.match(html, />null</);
  assert.match(html, /class-settings/);
  assert.match(html, /<option value="LISTBND">LISTBND<\/option><option value="TQBR">TQBR<\/option>/);
  assert.doesNotMatch(html, /Подписка|FIX|Стакан/);
});

test('an unavailable source shows an error, not stale or FIX data', () => {
  const html = renderPage('[]', new Error('market data unavailable'));
  assert.match(html, /error-banner/);
  assert.match(html, /Таблица Params недоступна/);
});
