const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = globalThis;
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
  const router = { Link: ({ to, className, children }) => React.createElement('a', { href: to, className }, children) };
  const jsx = { 'react/jsx-runtime': require('react/jsx-runtime') };
  const imports = {
    '../components/rawColumns': load('components/rawColumns.tsx', { ...jsx, '../lib/rawOrders': raw }),
    '../components/quikColumns': load('components/quikColumns.tsx', { ...jsx, 'react-router-dom': router, '../lib/rawOrders': raw, '../lib/quikFormat': load('lib/quikFormat.ts') }),
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    'react-router-dom': router,
    '../lib/quotes': load('lib/quotes.tsx', { 'react/jsx-runtime': require('react/jsx-runtime'), 'react-router-dom': router, './rawOrders': raw }),
    '@tanstack/react-query': { useQuery: () => ({ data: error ? undefined : rows, error, isLoading: false, isFetching: false, refetch() {} }) },
    '../api': { apiWithHeaders: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 2000 },
    '../lib/rawOrders': raw,
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true, subscribe: () => () => {} }) },
    '../components/PageHeader': { PageHeader: ({ title }) => React.createElement('header', null, title) },
    '../components/feedback': { ErrorBanner: () => React.createElement('p', null, 'error-banner') },
    '../components/DataTable': { DataTable: ({ rows, columns, filters, emptyTitle }) => {
      const visible = columns.filter(c => !c.defaultHidden);
      return React.createElement('section', null, filters,
        React.createElement('div', null, visible.map(c => React.createElement('b', { key: c.id }, c.header))),
        React.createElement('i', null, columns.filter(c => c.defaultHidden).map(c => c.header).join(',')),
        rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, visible.map(c => React.createElement('span', { key: c.id }, c.cell(r)), ))) : emptyTitle);
    } },
  };
  return renderToStaticMarkup(React.createElement(load('pages/MarketDataPage.tsx', imports).default));
}

test('Params rows show the chosen bond columns with exact values; the rest stay in the column menu', () => {
  const html = renderPage('[{"ClassCode":"LIST_GBA","SecCode":"GBA02270915","CouponValue":2.500000,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2","SecFullName":"ГКО-2 летние","ClassName":"Листинговые:облигации","valtoday":12345678.50000000},{"ClassCode":"TQBR","SecCode":"SBER","CouponValue":9007199254740993,"status":null,"FaceUnit":null,"SecShortName":null,"SecFullName":null,"ClassName":null,"valtoday":null}]');
  assert.match(html, /<b>Инструмент<\/b><b>Класс \/ валюта<\/b><b>Купон \/ дата<\/b><b>Оборот<\/b><\/div>/);
  assert.match(html, /<i>status,SecFullName,ClassName<\/i>/, 'other columns are hidden, not dropped');
  assert.match(html, />2,50</);
  assert.match(html, />9 007 199 254 740 993,00</, 'long numbers keep every digit');
  assert.match(html, />12 345 678,50</);
  assert.match(html, /class="cell-null">—</);
  // The class choice has its own page.
  assert.match(html, /<a href="\/classes" class="text-accent">«Классы QUIK»<\/a>/);
  // Each instrument links to its own page with all of its market data.
  assert.match(html, /<a href="\/market-data\/LIST_GBA\/GBA02270915" class="text-accent">GBA02270915<\/a><small>ГКО-2<\/small>/);
  assert.match(html, /<option value="LIST_GBA">LIST_GBA<\/option><option value="TQBR">TQBR<\/option>/);
  assert.doesNotMatch(html, /Подписка|FIX|Стакан/);
});

test('an unavailable source shows an error, not stale or FIX data', () => {
  const html = renderPage('[]', new Error('market data unavailable'));
  assert.match(html, /error-banner/);
  assert.match(html, /Таблица Params недоступна/);
});
