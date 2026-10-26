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
  const context = { exports: {}, JSON, Set, Array, Number, encodeURIComponent, require: name => { if (name in imports) return imports[name]; throw Error(name); } };
  vm.runInNewContext(source, context);
  return context.exports;
}
const raw = load('lib/rawOrders.ts');
const jsx = require('react/jsx-runtime');
const router = { Link: ({ to, className, children }) => React.createElement('a', { href: to, className }, children), useParams: () => ({ board: 'LISTBND', symbol: 'GBA02270915' }) };
const quotes = load('lib/quotes.tsx', { 'react/jsx-runtime': jsx, 'react-router-dom': router, './rawOrders': raw });
const table = { DataTable: ({ title, rows, columns, emptyTitle }) => React.createElement('section', null, React.createElement('h2', null, title),
  React.createElement('div', null, columns.map(c => React.createElement('b', { key: c.id }, c.header))),
  rows.length ? rows.map((r, i) => React.createElement('div', { key: i }, columns.map(c => React.createElement('span', { key: c.id }, c.cell ? c.cell(r) : String(c.value(r)))))) : emptyTitle) };
const common = {
  react: React, 'react/jsx-runtime': jsx, 'react-router-dom': router,
  '../lib/poll': { liveRefetchInterval: () => 2000 }, '../lib/rawOrders': raw, '../lib/quotes': quotes,
  '../components/PageHeader': { PageHeader: ({ title, subtitle }) => React.createElement('header', null, title, ' ', subtitle) },
  '../components/feedback': { ErrorBanner: () => React.createElement('p', null, 'error-banner'), PageSpinner: () => null },
  '../components/DataTable': table,
};

const quote = '{"OrderNum":7412,"ClassCode":"LIST_GD","TradeDate":"2026-10-26T00:00:00","SecCode":"GD052270215","Price":100.00000000,"Value":1000.00,"Balance":10.00000000,"Qty":10.00000000,"FirmId":"SENTI2004","Operation":"Купля","State":"Активна"}';

test('quotes come from QUIK Orders rows and link to the order and the instrument', () => {
  const page = load('pages/QuotesPage.tsx', { ...common,
    '@tanstack/react-query': {
      useQuery: ({ queryKey }) => ({ data: queryKey[0] === 'admin-quotes' ? raw.parseDBJSON('[' + quote + ']') : { base_url: 'http://x', default_base_url: 'http://x' }, isLoading: false, isFetching: false, refetch() {} }),
      useMutation: () => ({ isPending: false, mutate() {} }), useQueryClient: () => ({ invalidateQueries() {} }),
    },
    '../api': { api() { throw Error('unexpected request'); }, apiWithHeaders() { throw Error('unexpected request'); }, getSessionUser: () => ({ role: 'ADMIN' }) },
    '../components/Toast': { useToast: () => ({ push() {} }) },
  });
  const html = renderToStaticMarkup(React.createElement(page.default));
  assert.match(html, /Сегодняшние активные заявки QUIK · таблица Orders, State = «Активна»/);
  assert.match(html, /<a href="\/orders\/quik-20261026-LIST_GD-7412" class="text-accent">7412<\/a>/);
  assert.match(html, /<a href="\/market-data\/LIST_GD\/GD052270215" class="text-accent">GD052270215<\/a>/);
  assert.match(html, /SENTI2004/);
  assert.match(html, /<b>Остаток<\/b>/);
  assert.match(html, /2026-10-26/);
  assert.match(html, /история сделок/);
});

test('the instrument page shows every stored column of Params, Securities and Classes and its quotes', () => {
  const details = '{"params":{"ClassCode":"LISTBND","SecCode":"GBA02270915","last":100.5,"bid":null,"yield":12.345678901234567},"security":{"SecShortName":"ГКО-2","FaceUnit":"KGS"},"class":null,"quotes":[' + quote + '],"quotes_has_more":false}';
  const page = load('pages/MarketInstrumentPage.tsx', { ...common,
    '@tanstack/react-query': { useQuery: () => ({ data: raw.parseDBJSON(details), isLoading: false, error: null, refetch() {} }) },
    '../api': { api() { throw Error('unexpected request'); } },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: true }) },
  });
  const html = renderToStaticMarkup(React.createElement(page.default));
  assert.match(html, /LISTBND · GBA02270915/);
  assert.match(html, /ГКО-2/);
  for (const field of ['last', 'bid', 'yield', 'SecShortName', 'FaceUnit']) assert.match(html, new RegExp(`<th class="text-left font-mono">${field}</th>`));
  assert.match(html, />12\.345678901234567</);
  assert.match(html, /Нет записи в QUIK Classes/);
  assert.match(html, /<h2>Котировки<\/h2>/);
  assert.match(html, /7412/);
});
