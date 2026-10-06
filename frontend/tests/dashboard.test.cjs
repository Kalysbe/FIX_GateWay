const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

function renderDashboard(daily) {
  const imports = {
    react: React,
    'react/jsx-runtime': require('react/jsx-runtime'),
    '@tanstack/react-query': { useQuery: ({ queryKey }) => {
      if (queryKey[0] === 'dashboard') return daily;
      if (queryKey[0] === 'monitoring') return { data: { timestamp: '2026-10-05T09:00:00Z', counters: { orders_queue_size: 7 }, adapters: {}, heap_bytes: 0, goroutines: 12, cpu_cores: 4, database: null } };
      return { data: { series: { cpu_pct: [{ time: '2026-10-05T09:00:00Z', value: 12.5 }] }, adapters: {} } };
    } },
    'react-router-dom': { Link: ({ children, to }) => React.createElement('a', { href: to }, children) },
    '../api': { api: () => { throw Error('unexpected network request'); } },
    '../labels': {},
    '../lib/adapters': { isHiddenAdapter: a => !a.enabled, isSimulator: () => false, displayAdapterName: name => name },
    '../lib/poll': { liveRefetchInterval: () => 5000 },
    '../hooks/useAdminStream': { useAdminStream: () => ({ connected: false }) },
  };
  const feedback = { PageSpinner: () => React.createElement('p', null, 'Loading daily statistics'), ErrorBanner: ({ error }) => React.createElement('p', null, error.message), EmptyState: () => null };
  imports['./feedback'] = imports['../components/feedback'] = feedback;
  imports['./StatusBadge'] = imports['../components/StatusBadge'] = { StatusBadge: () => null };
  imports['../components/PageHeader'] = { PageHeader: ({ title }) => React.createElement('h1', null, title) };
  const load = path => {
    const compiled = ts.transpileModule(readFileSync(resolve(__dirname, path), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX } }).outputText;
    const context = { exports: {}, require: name => { if (!(name in imports)) throw Error(name); return imports[name]; } };
    vm.runInNewContext(compiled, context);
    return context.exports;
  };
  imports['../components/GatewayMonitoring'] = load('../src/components/GatewayMonitoring.tsx');
  return renderToStaticMarkup(React.createElement(load('../src/pages/DashboardPage.tsx').default));
}

test('dashboard displays runtime telemetry and server CPU rate while daily statistics load', () => {
  const html = renderDashboard({ isLoading: true });
  assert.match(html, /Нагрузка нашего сервера/);
  assert.match(html, /12,5%/);
  assert.match(html, /Очередь: 7/);
  assert.match(html, /Loading daily statistics/);
});

test('failed daily statistics do not hide live dashboard telemetry', () => {
  const html = renderDashboard({ isLoading: false, error: Error('Daily statistics unavailable') });
  assert.match(html, /Daily statistics unavailable/);
  assert.match(html, /Нагрузка нашего сервера/);
  assert.match(html, /12,5%/);
});
