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
const jsx = { 'react/jsx-runtime': require('react/jsx-runtime') };
const quikColumns = load('components/quikColumns.tsx', { ...jsx, 'react-router-dom': { Link: () => null }, '../lib/rawOrders': raw, '../lib/quikFormat': load('lib/quikFormat.ts') });

function renderPositions(data, error) {
  const queries = [];
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: options => {
      queries.push(options);
      return options.queryKey[0] === 'brokers' ? { data: { brokers: [] }, isLoading: false } : { data, error, isLoading: false };
    } },
    '../api': { api: () => { throw Error('unexpected network request'); }, apiWithHeaders: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 5000 },
    '../lib/rawOrders': raw,
    '../components/rawColumns': { rawSearchText: () => () => '' },
    '../components/quikColumns': quikColumns,
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
    '../components/PageHeader': { PageHeader: ({ title, subtitle }) => React.createElement('header', null, title, ' ', subtitle) },
    '../components/feedback': { ErrorBanner: () => 'ошибка источника' },
    '../components/DataTable': { DataTable: ({ rows, emptyTitle, emptyHint, columns }) => React.createElement('section', null,
      React.createElement('i', null, columns.filter(c => !c.defaultHidden).map(c => c.header).join(',')),
      rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.filter(c => !c.defaultHidden).map(c => React.createElement('span', { key: c.id }, `${c.id}=`, c.cell(r), ';')))) : [emptyTitle, emptyHint]),
    },
  };
  return { html: renderToStaticMarkup(React.createElement(load('pages/PositionsPage.tsx', imports).default)), queries };
}

test('positions show the chosen balance columns of QUIK rows, with both tabs counted', () => {
  const rows = raw.parseDBJSON('{"instruments":[{"FirmId":"2040","ClientCode":"C1","Trdacc":"1-3001-71","SecCode":"GBA02280810","LimitKind":"T0","OpenBal":0.00000000,"CurrentBal":17.50000000,"LockedBuy":0,"LockedSell":0,"AwgPositionPrice":0,"WAPositionPriceCurrency":null,"CurrentLimit":0},'
    + '{"FirmId":"2040","ClientCode":"C9","SecCode":"US1","OpenBal":0,"CurrentBal":0.00000000,"LockedBuy":0,"LockedSell":0}],'
    + '"cash":[{"FirmId":"2040","ClientCode":"C1","CurrCode":"KGS","CurrentBal":1518400.00000000},{"FirmId":"2040","ClientCode":"C2","CurrCode":"USD","CurrentBal":2}]}');
  const { html, queries } = renderPositions(rows);
  assert.match(html, /MoneyLimits/);
  assert.match(html, /Инструменты \(<!-- -->1<!-- -->\)|Инструменты \(1\)/, 'a row of zero balances is hidden by default');
  assert.match(html, /Деньги \(<!-- -->2<!-- -->\)|Деньги \(2\)/);
  assert.match(html, /<i>Фирма,Клиент \/ счёт,Инструмент,Срок,Входящий,Текущий,Блок. покупка,Блок. продажа,Ср. цена<\/i>/);
  assert.match(html, /client=<span class="cell-stack">C1<small>1-3001-71<\/small><\/span>;/);
  assert.match(html, /current=17,5;/);
  assert.doesNotMatch(html, /US1/);
  assert.match(html, /Скрыть нулевые остатки/);
  assert.doesNotMatch(html, /Подписка|снимок|FIX/);
  assert.deepEqual(queries.map(q => Array.from(q.queryKey)), [['brokers'], ['admin-positions', '']]);
});

test('an unavailable source is shown as such, not as empty positions', () => {
  const { html } = renderPositions(undefined, new Error('503'));
  assert.match(html, /ошибка источника/);
  assert.match(html, /Позиции временно недоступны/);
  assert.doesNotMatch(html, /В этой выборке позиций нет/);
});
