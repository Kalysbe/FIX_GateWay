const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src/pages/PositionsPage.tsx'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText;

function renderPositions(data, error) {
  const queries = [];
  const value = v => v == null ? 'null' : String(v);
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: options => {
      queries.push(options);
      return options.queryKey[0] === 'brokers' ? { data: { brokers: [] }, isLoading: false } : { data, error, isLoading: false };
    } },
    '../api': { api: () => { throw Error('unexpected network request'); }, apiWithHeaders: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 5000 },
    '../lib/rawOrders': { dbValue: value, parseDBJSON: JSON.parse },
    '../components/rawColumns': {
      rawColumns: rows => Array.from(new Set(rows.flatMap(r => Object.keys(r)))).map(id => ({ id, value: r => value(r[id]) })),
      rawSearchText: () => () => '',
    },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
    '../components/PageHeader': { PageHeader: ({ title, subtitle }) => React.createElement('header', null, title, ' ', subtitle) },
    '../components/feedback': { ErrorBanner: () => 'ошибка источника' },
    '../components/DataTable': { DataTable: ({ rows, emptyTitle, columns }) => React.createElement('section', null,
      rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, `${c.id}=${c.value(r)};`)))) : emptyTitle),
    },
  };
  const context = { exports: {}, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return { html: renderToStaticMarkup(React.createElement(context.exports.default)), queries };
}

test('positions show QUIK rows as stored, with both tabs counted', () => {
  const instruments = [{ FirmId: '2040', ClientCode: 'C1', SecCode: 'GBA02280810', CurrentBal: 17.5, WAPositionPriceCurrency: null }];
  const cash = [{ FirmId: '2040', ClientCode: 'C1', CurrCode: 'KGS', CurrentBal: 1 }, { FirmId: '2040', ClientCode: 'C2', CurrCode: 'USD', CurrentBal: 2 }];
  const { html, queries } = renderPositions({ instruments, cash });
  assert.match(html, /MoneyLimits/);
  assert.match(html, /Инструменты \(<!-- -->1<!-- -->\)|Инструменты \(1\)/);
  assert.match(html, /Деньги \(<!-- -->2<!-- -->\)|Деньги \(2\)/);
  assert.match(html, /SecCode=GBA02280810;/);
  assert.match(html, /CurrentBal=17\.5;/);
  assert.match(html, /WAPositionPriceCurrency=null;/);
  assert.doesNotMatch(html, /Подписка|снимок|FIX/);
  assert.deepEqual(queries.map(q => Array.from(q.queryKey)), [['brokers'], ['admin-positions', '']]);
});

test('an unavailable source is shown as such, not as empty positions', () => {
  const { html } = renderPositions(undefined, new Error('503'));
  assert.match(html, /ошибка источника/);
  assert.match(html, /Позиции временно недоступны/);
  assert.doesNotMatch(html, /В этой выборке позиций нет/);
});
