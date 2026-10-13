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
  const context = { exports: {}, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const market = load('lib/marketData.ts');
const seeds = ['KG0105166315', 'KG0106166315', 'GBA02280810'].map(symbol => ({
  symbol, security_id: symbol, board: 'LISTEDSTOCKK', trading_status: '',
  trading_session_id: '', md_req_id: '', last: '', bid: '', offer: '',
  updated_at: '2026-10-07T04:05:23Z',
}));
const totals = rows => Object.fromEntries(market.marketDataSummary(rows).map(s => [s.label, s.value]));

test('seeded subscription rows do not prove market data reception or zero trading instruments', () => {
  assert.equal(market.instrumentStatus('', '').label, 'Нет статуса');
  assert.equal(market.instrumentStatus('', '').reported, false);
  assert.equal(seeds.some(market.hasReceivedMarketData), false);
  assert.equal(totals(seeds)['Данные получены'], 0);
  assert.equal(totals(seeds)['Торгуются'], '—');
  assert.equal(totals(seeds)['Без статуса'], 3);
});

test('ARQA status codes and session identifiers have consistent labels, colours and totals', () => {
  assert.equal(market.instrumentStatus(' 17 ', 'OPEN').trading, true);
  assert.equal(market.instrumentStatus('', ' open ').label, 'Идут торги');
  assert.equal(market.instrumentStatus('18', 'OPEN').label, 'Закрыты');
  assert.equal(market.instrumentStatus('2').tone, 'badge-warn');
  assert.equal(market.instrumentStatus('', 'CLOSING_AUCTION').label, 'Аукцион закрытия');
  assert.equal(market.instrumentStatus('999', 'OPEN').trading, false);
  assert.equal(market.instrumentStatus('999').label, 'Статус 999');
  const rows = [
    { trading_status: '17' }, { trading_session_id: 'OPEN' },
    { trading_status: '18' }, { trading_status: '2' }, {},
  ];
  assert.equal(totals(rows)['Торгуются'], 2);
  assert.equal(totals(rows)['Без статуса'], 1);
});

test('a received empty order book and zero values count as data without inventing a status', () => {
  assert.equal(market.hasReceivedMarketData({ md_req_id: 'MD1', bids: [], offers: [] }), true);
  assert.equal(market.hasReceivedMarketData({ last: '0', volume: '0' }), true);
  assert.equal(market.hasReceivedMarketData({ trading_status: '18' }), true);
  assert.equal(market.instrumentStatus().reported, false);
});

function renderMarketPage(quotes, subscription) {
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': {
      useQuery: () => ({ data: { source: 'live', live: true, quotes, subscription, snapshot: { adapter_id: 'quik-fix-1', md_req_id: '', requested_at: '2026-10-07T04:09:57Z', report_count: quotes.length } }, isLoading: false }),
      useQueryClient: () => ({}),
    },
    '../api': { api: () => { throw Error('unexpected network request'); } },
    '../lib/poll': { liveRefetchInterval: () => 2000 },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true, subscribe: () => () => {} }) },
    '../components/PageHeader': { PageHeader: ({ title, actions }) => React.createElement('header', null, title, actions) },
    '../components/feedback': { ErrorBanner: () => null },
    '../lib/table': { numberValue: value => value === '' ? undefined : Number(value) },
    '../lib/marketData': market,
    '../components/DataTable': { DataTable: ({ rows, columns, summary }) => React.createElement('section', null,
      rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, c.cell ? c.cell(r) : c.value(r))))),
      summary(rows).map(s => React.createElement('p', { key: s.label }, `${s.label}: ${s.value}`))),
    },
  };
  return renderToStaticMarkup(React.createElement(load('pages/MarketDataPage.tsx', imports).default));
}

test('connected console with seeded quotes shows missing data instead of a green market confirmation', () => {
  const html = renderMarketPage(seeds);
  assert.match(html, /Консоль подключена/);
  assert.match(html, /Рыночные данные не получены по 3 из 3 инструментов/);
  assert.equal((html.match(/>Нет статуса</g) || []).length, 3);
  assert.match(html, /Торгуются: —/);
  assert.match(html, /Без статуса: 3/);
  assert.match(html, /Подписка и восстановление выполняются автоматически/);
  assert.doesNotMatch(html, /Проверить подписку|Подписаться по FIX|>Обновить<|Код инструмента|Торговый класс|>Онлайн</);
});

test('subscription rejection shows automatic recovery and the upstream reason without manual controls', () => {
  const html = renderMarketPage(seeds, { status:'RETRYING', expected:3, confirmed:0, error:'Unknown ExDestination [BOARD]', next_retry_at:'2026-10-07T04:10:00Z' });
  assert.match(html, /Восстановление потока/);
  assert.match(html, /Автоматическое восстановление потока/);
  assert.match(html, /Unknown ExDestination \[BOARD\]/);
  assert.match(html, /следующая проверка/);
  assert.doesNotMatch(html, /Проверьте подписку|Проверить подписку|Подписаться/);
});

test('reported trading status renders even without quotes and removes the missing-status warning', () => {
  const html = renderMarketPage([{ ...seeds[0], trading_status: '17', md_req_id: 'MD1' }]);
  assert.match(html, />Идут торги</);
  assert.match(html, /Торгуются: 1/);
  assert.match(html, /Без статуса: 0/);
  assert.doesNotMatch(html, /Статус не передан|Рыночные данные не получены/);
});
