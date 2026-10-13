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

function renderPositions(data) {
  const queries = [];
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: options => {
      queries.push(options);
      return { data: options.queryKey[0] === 'brokers' ? { brokers: [] } : data, isLoading: false };
    } },
    'react-router-dom': { Link: ({ to, children }) => React.createElement('a', { href: to }, children) },
    '../api': { api: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 5000 },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
    '../components/PageHeader': { PageHeader: ({ title, actions }) => React.createElement('header', null, title, actions) },
    '../components/feedback': { ErrorBanner: () => null },
    '../lib/table': { numberValue: value => value === '' ? undefined : Number(value) },
    '../components/DataTable': { DataTable: ({ rows, emptyTitle, emptyHint, columns, summary }) => React.createElement('section', null,
      rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, c.cell ? c.cell(r) : c.value(r))))) : `${emptyTitle}. ${emptyHint}`,
      summary(rows).map(s => React.createElement('p', { key: s.label }, `${s.label}: ${s.value}`))),
    },
  };
  const context = { exports: {}, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return { html: renderToStaticMarkup(React.createElement(context.exports.default)), queries };
}

test('positions are obtained automatically without manual FIX or refresh controls', () => {
  const { html, queries } = renderPositions({ source: 'fix', live: true, instruments: [], cash: [], subscription: { status: 'ACTIVE', positions: 0 } });
  assert.match(html, /Подписка активна/);
  assert.match(html, /В этой выборке позиций нет/);
  assert.match(html, /Получение позиций и восстановление потока выполняются автоматически/);
  assert.doesNotMatch(html, /Запросить по FIX|Запросите актуальный|>Обновить<|Нет связи/);
  assert.deepEqual(queries.map(q => Array.from(q.queryKey)), [['brokers'], ['admin-positions', '']]);
});

test('recovering position stream labels saved data and exposes automatic recovery reason', () => {
  const row = { kind: 'INSTRUMENT', firm_id: 'FIRM', client_code: 'CLIENT', account_code: 'ACC', symbol: 'TEST', board: 'BOARD', currency: 'KGS', net_qty: '12', long_qty: '12', short_qty: '0' };
  const { html } = renderPositions({ source: 'fix', live: false, instruments: [row], cash: [], snapshot: { adapter_id: 'a1', requested_at: '2026-10-07T04:00:00Z', report_count: 1 }, subscription: { status: 'RETRYING', positions: 0, error: 'upstream rejection', next_retry_at: '2026-10-07T04:00:05Z' } });
  assert.match(html, /Восстановление позиций/);
  assert.match(html, /Сохранённый снимок/);
  assert.match(html, /Показан последний сохранённый снимок/);
  assert.match(html, /upstream rejection/);
  assert.match(html, /следующая проверка/);
  assert.match(html, /TEST/);
  assert.doesNotMatch(html, /Подписка активна|Запросить по FIX/);
});

test('positions wait automatically for first complete snapshot without inventing a live feed', () => {
  const { html } = renderPositions({ source: 'fix', live: false, instruments: [], cash: [], subscription: { status: 'STARTING', positions: 0 } });
  assert.match(html, /Получение позиций/);
  assert.match(html, /Ожидание позиций/);
  assert.doesNotMatch(html, /Подписка активна|В этой выборке позиций нет|Запросить по FIX/);
});

test('automatic snapshot fallback is shown without claiming a confirmed stream', () => {
  const { html } = renderPositions({ source: 'fix', live: true, instruments: [], cash: [], subscription: { status: 'ACTIVE', mode: 'snapshot', positions: 0 } });
  assert.match(html, /Автообновление активно/);
  assert.match(html, /В этой выборке позиций нет/);
  assert.doesNotMatch(html, /Подписка активна|Запросить по FIX|>Обновить</);
});
