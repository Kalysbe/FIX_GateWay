const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

function load(file, imports = {}, globals = {}) {
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src', file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  const context = { ...globals, exports: {}, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const { groupDeals, tradeRows, exportDeals } = load('lib/trades.ts');
const table = load('lib/table.ts');
const leg = (id, side, changes = {}) => ({
  id, order_id: id, exec_id: `LIST_GBA-2sw-${side === 'BUY' ? 'B' : 'S'}-1-1-N`, side,
  created_at: side === 'BUY' ? '2026-10-06T10:00:00Z' : '2026-10-06T12:20:33Z',
  execution_date: '2026-10-06', symbol: 'GBA02280810', board: 'LIST_GBA',
  last_qty: '201', last_px: '99', cum_qty: '201', leaves_qty: '0', ...changes,
});

test('a late sell and older buy render as BUY then SELL without mutating responses', () => {
  const input = [leg('sell', 'SELL'), leg('buy', 'BUY')];
  const deals = groupDeals(input);
  assert.equal(deals.length, 1);
  assert.equal(deals[0].paired, true);
  assert.deepEqual(Array.from(deals[0].trades, t => t.id), ['buy', 'sell']);
  assert.equal(input[0].id, 'sell');
});

test('reused execution ids on different trade dates, instruments and boards never form a false pair', () => {
  const sell = leg('sell', 'SELL');
  for (const changes of [
    { execution_date: '2026-10-07' }, { symbol: 'OTHER' }, { board: 'OTHER' },
    { exec_id: 'LIST_GBA-2sw-B-1-1-N', side: 'SELL' },
  ]) {
    const deals = groupDeals([sell, leg('other', 'BUY', changes)]);
    assert.equal(deals.some(d => d.paired), false);
  }
});

test('explicit execution date pairs delayed reports across receipt dates and legacy dates use UTC', () => {
  assert.equal(groupDeals([leg('sell', 'SELL'), leg('buy', 'BUY', { created_at: '2026-10-07T00:01:00Z' })])[0].paired, true);
  const legacy = [leg('sell', 'SELL', { execution_date: undefined }), leg('buy', 'BUY', { execution_date: undefined })];
  assert.equal(groupDeals(legacy)[0].paired, true);
  assert.equal(groupDeals([legacy[0], { ...legacy[1], created_at: '2026-10-07T00:01:00Z' }]).length, 2);
});

test('side-like market prefixes remain intact and opaque ids are never used to invent pairs', () => {
  const trades = [leg('sell', 'SELL'), leg('buy', 'BUY')];
  assert.equal(groupDeals(trades.map(t => ({ ...t, exec_id: `MARKET-B-${t.exec_id}` })))[0].paired, true);
  assert.equal(groupDeals(trades.map(t => ({ ...t, exec_id: 'opaque-id' }))).length, 2);
});

test('sorting, filtering, search, pagination and CSV keep both legs of each match together', () => {
  const input = Array.from({ length: 21 }, (_, n) => ['SELL', 'BUY'].map(side => leg(`${n}-${side}`, side, {
    exec_id: `LIST_GBA-${n}-${side === 'BUY' ? 'B' : 'S'}-1-1-N`, client_code: side === 'BUY' ? `buyer-${n}` : `seller-${n}`,
  }))).flat();
  const deals = groupDeals(input);
  const found = table.filterRows(deals, 'buyer-20', d => d.trades.map(t => t.client_code).join(' '));
  assert.equal(found.length, 1);
  assert.equal(found[0].trades.length, 2);
  const sorted = table.sortRows(deals, d => d.label, 'desc');
  const pages = [table.paginateRows(sorted, 0, 20), table.paginateRows(sorted, 1, 20)];
  assert.equal(pages[0].rows.length, 20);
  assert.equal(pages[1].rows.length, 1);
  assert.ok(pages.every(p => p.rows.every(d => d.trades[0].side === 'BUY' && d.trades[1].side === 'SELL')));

  let props, coverage;
  const states = [];
  let index = 0;
  const imports = {
    react: { ...React, useMemo: fn => fn(), useState: initial => {
      const i = index++;
      if (!(i in states)) states[i] = initial;
      return [states[i], value => { states[i] = typeof value === 'function' ? value(states[i]) : value; }];
    } }, 'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: () => ({ data: { trades: input, coverage }, refetch: () => {} }) },
    'react-router-dom': { Link: ({ to, ...p }) => React.createElement('a', { href: to, ...p }) },
    '../api': {}, '../labels': { sideLabel: s => s, formatDateTime: s => s },
    '../lib/poll': { liveRefetchInterval: () => false }, '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
    '../components/PageHeader': { PageHeader: () => null }, '../components/feedback': { ErrorBanner: () => null },
    '../components/DataTable': { DataTable: p => { props = p; return null; } },
    '../lib/table': table, '../lib/trades': { groupDeals, tradeRows, exportDeals },
  };
  const Page = load('pages/TradesPage.tsx', imports).default;
  const renderPage = () => { index = 0; return renderToStaticMarkup(React.createElement(Page)); };
  renderPage();
  React.Children.toArray(props.filters.props.children).find(el => el.type === 'select').props.onChange({ target: { value: 'BUY' } });
  renderPage();
  assert.equal(props.rows.length, 21);
  assert.ok(props.rows.every(d => d.trades.length === 2));
  const html = renderToStaticMarkup(React.createElement('table', null, React.createElement('tbody', null, props.renderRow(found[0], 0))));
  assert.ok(html.indexOf('>BUY<') < html.indexOf('>SELL<'));
  assert.equal((html.match(/data-paired="true"/g) || []).length, 2);
  assert.match(html, /href="\/trades\/20-BUY"/);
  assert.match(html, /href="\/trades\/20-SELL"/);
  assert.doesNotMatch(html, /Вторая сторона не получена/);
  const csv = props.exportRows(found);
  assert.deepEqual(Array.from(csv.rows, row => row[11]), ['BUY', 'SELL']);
  assert.equal(csv.rows[0][0], csv.rows[1][0]);
  const single = groupDeals([leg('single', 'SELL')])[0];
  const singleHtml = renderToStaticMarkup(React.createElement('table', null, React.createElement('tbody', null, props.renderRow(single, 0))));
  assert.doesNotMatch(singleHtml, /Контрагент/);
  assert.equal((singleHtml.match(/<tr /g) || []).length, 1);
  assert.doesNotMatch(singleHtml, /Вторая сторона не получена|orders\/undefined/);
  coverage = { trades: 21, sides: 42, complete: 21, incomplete: 0, report_errors: 0, last_sync_at: 'new-failed-attempt', last_success_at: 'older-success', sync_stale: true, last_sync_error: 'history coverage regressed' };
  const staleHtml = renderPage();
  assert.match(staleHtml, /Актуальная полнота загрузки не подтверждена/);
  assert.match(staleHtml, /Полнота подтверждена: older-success/);
  assert.match(staleHtml, /history coverage regressed/);
  assert.doesNotMatch(staleHtml, /Полнота подтверждена: new-failed-attempt/);
  coverage = { ...coverage, last_success_at: 'fresh-success', sync_stale: false, last_sync_error: '' };
  assert.doesNotMatch(renderPage(), /Актуальная полнота загрузки не подтверждена/);
});

function detailHarness(response) {
  let options, breadcrumbs, prints = 0;
  const requests = [];
  class ApiError extends Error { constructor(status) { super('API error'); this.status = status; } }
  const Page = load('pages/TradeDetailPage.tsx', {
    react: React, 'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: opts => { options = opts; return response(ApiError); } },
    'react-router-dom': { useParams: () => ({ id: 'historical-execution' }), Link: ({ to, ...p }) => React.createElement('a', { href: to, ...p }) },
    '../api': { api: async (url, init) => requests.push({ url, init }), ApiError },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
    '../lib/poll': { liveRefetchInterval: () => false }, '../lib/trades': { groupDeals, tradeRows, exportDeals },
    '../labels': { sideLabel: s => s, formatDate: d => d, formatDateTime: d => d },
    '../components/breadcrumbs': { useBreadcrumbs: value => { breadcrumbs = value; } },
    '../components/PageHeader': { PageHeader: ({ title, actions }) => React.createElement('header', null, title, actions) },
    '../components/feedback': { EmptyState: ({ title }) => React.createElement('p', null, title), ErrorBanner: ({ onRetry }) => React.createElement('button', { onClick: onRetry }, 'Повторить'), PageSpinner: ({ label }) => React.createElement('p', null, label) },
  }, { window: { print: () => { prints++; } } }).default;
  const tree = Page();
  function find(n, predicate) {
    if (!n || typeof n !== 'object') return [];
    if (Array.isArray(n)) return n.flatMap(el => find(el, predicate));
    return [...(predicate(n) ? [n] : []), ...find(n.props?.children, predicate), ...find(n.props?.actions, predicate)];
  }
  return { tree, options, breadcrumbs, requests, get prints() { return prints; }, find: predicate => find(tree, predicate), html: () => renderToStaticMarkup(tree), ApiError };
}

test('trade detail loads its own endpoint, prints both complete legs and links their orders', async () => {
  const trades = [leg('sell', 'SELL'), leg('buy', 'BUY', { last_qty: '9007199254740993.00000001', fix_cl_ord_id: 'FIX-B', quik_order_id: 'QUIK-B' })];
  const h = detailHarness(() => ({ data: { trades } }));
  await h.options.queryFn({ signal: 'test-signal' });
  assert.equal(h.requests[0].url, '/api/v1/admin/trades/historical-execution');
  const html = h.html();
  assert.ok(html.indexOf('>BUY<') < html.indexOf('>SELL<'));
  assert.match(html, /9007199254740993\.00000001/);
  assert.match(html, /FIX-B/);
  assert.match(html, /QUIK-B/);
  assert.match(html, /href="\/orders\/buy"/);
  assert.match(html, /href="\/orders\/sell"/);
  assert.equal(h.breadcrumbs[0].to, '/trades');
  h.find(n => n.type === 'button' && n.props.children === 'Печать')[0].props.onClick();
  assert.equal(h.prints, 1);
});

test('trade detail distinguishes loading, missing, failed and incomplete matches before printing', () => {
  const loading = detailHarness(() => ({ isLoading: true }));
  assert.match(loading.html(), /Загрузка сделки/);
  assert.doesNotMatch(loading.html(), /Печать/);
  const missing = detailHarness(ApiError => ({ error: new ApiError(404) }));
  assert.match(missing.html(), /Сделка не найдена/);
  assert.doesNotMatch(missing.html(), /Печать/);
  assert.equal(missing.options.retry(0, new missing.ApiError(404)), false);
  const failed = detailHarness(() => ({ error: Error('unavailable') }));
  assert.match(failed.html(), /Повторить/);
  const single = detailHarness(() => ({ data: { trades: [leg('sell', 'SELL')] }, isFetching: true }));
  assert.match(single.html(), /В БД пока нет записи одной из сторон/);
  assert.doesNotMatch(single.html(), /Вторая сторона не получена|Одна сторона получена|orders\/undefined/);
  assert.equal(single.find(n => n.type === 'button' && n.props.children === 'Печать')[0].props.disabled, true);
});

test('TradeMatchID groups different execution ids and separates different actual matches', () => {
  const id = '261006-LIST_GBA-11094';
  const buy = leg('buy', 'BUY', { exec_id: 'opaque-buy', trade_match_id: id });
  const sell = leg('sell', 'SELL', { exec_id: 'unrelated-sell', trade_match_id: id });
  assert.equal(groupDeals([sell, buy])[0].paired, true);
  assert.equal(groupDeals([sell, buy])[0].label, id);
  assert.equal(groupDeals([leg('b', 'BUY', { trade_match_id: 'M1' }), leg('s', 'SELL', { trade_match_id: 'M2' })]).length, 2);
});

test('only actual persisted sides render and recovered QUIK buyer retains its order and client', () => {
  const report = leg('sell', 'SELL', { ledger_trade_id: 'ledger-id', quik_trade_num: '11094', trade_match_id: '261006-LIST_GBA-11094', quik_firm: '2040', firm_name: 'Freedom', contra_broker: '2040', client_code: 'imar', has_fix_report: true, has_quik_record: true });
  const incomplete = groupDeals([report])[0];
  assert.equal(tradeRows(incomplete).length, 1);
  assert.equal(incomplete.paired, false);
  const buyer = leg('real-buyer-id', 'BUY', { ledger_trade_id: 'ledger-id', quik_trade_num: '11094', trade_match_id: report.trade_match_id, order_id: null, exec_id: '', quik_order_num: '22450', client_code: 'Kalys', has_fix_report: false, has_quik_record: true, cum_qty: null, leaves_qty: null });
  const deal = groupDeals([report, buyer])[0];
  const rows = tradeRows(deal);
  assert.deepEqual(Array.from(rows, r => r.side), ['BUY', 'SELL']);
  assert.equal(rows[0].id, 'real-buyer-id');
  assert.equal(rows[0].detail_id, 'ledger-id');
  assert.equal(rows[0].client_code, 'Kalys');
  assert.equal(rows[0].quik_order_num, '22450');
  const csv = exportDeals([deal]);
  assert.equal(csv.rows.length, 2);
  assert.deepEqual(Array.from(csv.rows, r => r[17]), ['quik', 'fix+quik']);
  assert.equal(csv.rows[0][4], null);
  const detail = detailHarness(() => ({ data: { trades: [report, buyer] } }));
  assert.match(detail.html(), /Kalys/);
  assert.match(detail.html(), /22450/);
  assert.match(detail.html(), /Покупка и продажа сохранены/);
  assert.doesNotMatch(detail.html(), /counterparty:|Контрагент · FIX|orders\/null/);
});
