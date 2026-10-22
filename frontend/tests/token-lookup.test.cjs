const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

class ApiError extends Error { constructor(message, status) { super(message); this.status = status; } }

function harness(detail = {}) {
  const state = [], requests = [], pending = [], queries = [];
  let index = 0, selected = null, effectStarted = false, cleanup;
  const source = ts.transpileModule(readFileSync(resolve(__dirname, '../src/components/TokenLookup.tsx'), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
  }).outputText;
  const ctx = { exports: {}, Error, AbortController, require: name => {
    if (name === 'react') return { ...React,
      useState: initial => { const i = index++; if (!(i in state)) state[i] = initial; return [state[i], v => { state[i] = typeof v === 'function' ? v(state[i]) : v; }]; },
      useRef: initial => { const i = index++; if (!(i in state)) state[i] = { current: initial }; return state[i]; },
      useEffect: fn => { if (!effectStarted) { effectStarted = true; cleanup = fn(); } },
    };
    if (name === 'react/jsx-runtime') return require(name);
    if (name === '@tanstack/react-query') return { useQuery: options => { queries.push(options); return { isLoading: false, isFetching: false, refetch: async () => {}, ...detail }; } };
    if (name === '../api') return { ApiError, api: (path, init) => { requests.push({ path, ...init }); return new Promise((resolve, reject) => pending.push({ resolve, reject })); } };
    if (name === '../labels') return { formatDateTime: value => value || '—' };
    if (name === 'react-router-dom') return { Link: ({ to, children, ...props }) => React.createElement('a', { href: to, ...props }, children) };
    if (name === './icons') return { Icons: new Proxy({}, { get: () => () => null }) };
    if (name === './StatusBadge') return { StatusBadge: ({ status }) => React.createElement('span', {}, status) };
    if (name === './feedback') return { PageSpinner: ({ label }) => React.createElement('p', {}, label), ErrorBanner: () => React.createElement('p', {}, 'Load error') };
    throw new Error(name);
  } };
  vm.runInNewContext(source, ctx);
  function render() { index = 0; return ctx.exports.TokenLookup({ tokenId: selected, onSelect: id => { selected = id; } }); }
  function nodes(tree, predicate) { const out = []; function visit(n) { if (!n || typeof n !== 'object') return; if (Array.isArray(n)) return n.forEach(visit); if (predicate(n)) out.push(n); visit(n.props?.children); } visit(tree); return out; }
  const find = predicate => nodes(render(), predicate);
  const type = value => find(n => n.type === 'input')[0].props.onChange({ target: { value } });
  const submit = () => find(n => n.type === 'form')[0].props.onSubmit({ preventDefault() {} });
  const text = () => renderToStaticMarkup(render());
  return { render, find, type, submit, text, requests, pending, queries, selected: () => selected, cleanup: () => cleanup?.() };
}

const id = '00000000-0000-4000-8000-000000000001';
const metadata = { id, name: 'Main token', token_prefix: 'prefix12', permissions: ['orders:read'], status: 'EXPIRED', created_at: '2026-10-01T00:00:00Z', expires_at: '2026-10-20T00:00:00Z', quik_firm: '2040' };

test('lookup submits the credential only in POST body and clears it before awaiting the result', async () => {
  const h = harness(); const secret = 'synthetic_credential_' + 'a'.repeat(44);
  h.type(secret); const submitted = h.submit();
  assert.equal(h.find(n => n.type === 'form')[0].props.method, 'post');
  assert.equal(h.requests[0].path, '/api/v1/admin/tokens/search');
  assert.equal(h.requests[0].method, 'POST');
  assert.equal(JSON.parse(h.requests[0].body).token, secret);
  assert.equal(h.requests[0].cache, 'no-store');
  assert.equal(h.find(n => n.type === 'input')[0].props.value, '');
  assert.equal(h.find(n => n.type === 'input')[0].props.type, 'password');
  assert.ok(h.queries.every(q => !JSON.stringify(q.queryKey).includes(secret)));
  await h.submit(); assert.equal(h.requests.length, 1);
  h.pending[0].resolve({ tokens: [metadata], has_more: false }); await submitted;
  assert.equal(h.selected(), id);
  assert.equal(h.queries.at(-1).enabled, false); h.render();
  assert.equal(h.queries.at(-1).enabled, true);
  assert.deepEqual(JSON.parse(JSON.stringify(h.queries.at(-1).queryKey)), ['admin-token-overview', id]);
  assert.ok(!h.text().includes(secret));
});

test('ambiguous prefixes require explicit selection, while an empty result is a normal state', async () => {
  const h = harness(); h.type('prefix12'); const submitted = h.submit();
  const other = { ...metadata, id: '00000000-0000-4000-8000-000000000002', name: 'Other token' };
  h.pending[0].resolve({ tokens: [metadata, other], has_more: true }); await submitted;
  assert.equal(h.selected(), null); assert.match(h.text(), /первые 20/);
  h.find(n => n.type === 'button' && n.props.children?.[0]?.props?.children?.[0]?.props?.children === 'Other token')[0].props.onClick();
  assert.equal(h.selected(), other.id);
  h.type('missing1'); const next = h.submit(); h.pending[1].resolve({ tokens: [], has_more: false }); await next;
  assert.equal(h.selected(), null); assert.match(h.text(), /Токен не найден/);
});

test('cancelled searches cannot replace a later result or clear its loading state', async () => {
  const h = harness(); h.type('prefix12'); const first = h.submit();
  h.find(n => n.type === 'button' && n.props.children === 'Отменить')[0].props.onClick();
  assert.equal(h.requests[0].signal.aborted, true);
  h.type('different_prefix'); const second = h.submit();
  h.pending[0].resolve({ tokens: [metadata], has_more: false }); await first;
  assert.equal(h.selected(), null); assert.equal(h.find(n => n.type === 'form')[0].props['aria-busy'], true);
  h.pending[1].resolve({ tokens: [], has_more: false }); await second;
  assert.match(h.text(), /Токен не найден/);
});

test('search errors never echo the submitted token and unmount aborts an in-flight request', async () => {
  const h = harness(); const secret = 'private_fixture_value'; h.type(secret); const submitted = h.submit();
  h.pending[0].reject(new ApiError(secret, 400)); await submitted;
  assert.match(h.text(), /Введите полный API-токен/); assert.ok(!h.text().includes(secret));
  h.type(secret); const next = h.submit(); h.cleanup();
  assert.equal(h.requests[1].signal.aborted, true);
  h.pending[1].resolve({ tokens: [metadata], has_more: false }); await next;
  assert.equal(h.selected(), null);
});

test('overview shows ownership, rights, expiry and restrictions without inventing token activity', async () => {
  const h = harness({ data: { token: metadata, broker: { id: 7, name: 'Test broker', code: 'TEST', status: 'DISABLED', ip_whitelist_enabled: true, rate_limit_rps: 100, rate_limit_orders_per_sec: 20 }, ip_whitelist: [] } });
  h.type('prefix12'); const submitted = h.submit(); h.pending[0].resolve({ tokens: [metadata], has_more: false }); await submitted;
  const html = h.text();
  for (const value of ['Test broker', 'EXPIRED', '2040', 'Просмотр заявок и рынка', 'Нет данных об использовании', 'доступ закрыт', 'Лимиты брокера']) assert.ok(html.includes(value), value);
  assert.ok(!html.includes('token_hash'));
});
