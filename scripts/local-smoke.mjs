import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';

// Only the isolated Compose project is eligible for this write test.
const base = 'http://127.0.0.1:28080';
const compose = ['compose', '-f', 'deploy/docker-compose.local.yml'];
const docker = process.env.DOCKER_BIN || 'docker';

async function request(path, token, method = 'GET', body, expected = 200) {
  const response = await fetch(base + path, {
    method,
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(15000),
  });
  const result = await response.json();
  assert.equal(response.status, expected, `${method} ${path}: ${response.status} ${result.error || ''}`);
  return result;
}

async function waitStatus(id, token, status) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const result = await request(`/api/v1/orders/${id}`, token);
    if (result.order.status === status) return result;
    if (['FAILED', 'REJECTED', 'CANCELLED', 'FILLED', 'EXPIRED'].includes(result.order.status)) {
      assert.fail(`Expected ${status}, got ${result.order.status}: ${result.order.reject_reason || ''}`);
    }
    await delay(100);
  }
  assert.fail(`Timed out waiting for ${status}`);
}

async function watchEvents(token) {
  const controller = new AbortController();
  const response = await fetch(base + '/api/v1/admin/events/stream', {
    headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
    signal: controller.signal,
  });
  assert.equal(response.status, 200, 'SSE adapter must connect through native mTLS gRPC');
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let received = '';
  let failure;
  const reading = (async () => {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      received += decoder.decode(value, { stream: true });
      if (received.length > 1024 * 1024) received = received.slice(-512 * 1024);
    }
  })().catch((error) => { if (!controller.signal.aborted) failure = error; });
  return {
    async waitFor(text) {
      const deadline = Date.now() + 15000;
      while (!received.includes(text) && !failure && Date.now() < deadline) await delay(100);
      if (failure) throw failure;
      assert.ok(received.includes(text), `Realtime stream missing ${text}`);
    },
    async close() { controller.abort(); await reading; reader.releaseLock(); },
  };
}

async function main() {
  const config = JSON.parse(execFileSync(docker, [...compose, 'config', '--format', 'json'], { encoding: 'utf8' }));
  assert.equal(config.name, 'fix-gateway-local');
  assert.equal(config.networks.default.internal, true);
  assert.deepEqual(Object.keys(config.services.gateway.networks), ['default']);
  const containersRaw = execFileSync(docker, [...compose, 'ps', '--format', 'json', 'access'], { encoding: 'utf8' }).trim();
  const containers = containersRaw.startsWith('[') ? JSON.parse(containersRaw) : containersRaw.split(/\r?\n/).map(JSON.parse);
  assert.ok(containers.some((container) => container.Project === 'fix-gateway-local'
    && container.State === 'running'
    && container.Publishers.some((port) => port.TargetPort === 8080 && port.PublishedPort === 28080)),
  'The isolated access proxy must own the smoke endpoint');
  await request('/ready');
  const login = await request('/api/v1/admin/login', '', 'POST', {
    email: 'admin@localhost', password: 'admin123',
  });
  const admin = login.token;
  assert.ok(admin, 'Missing admin token');
  const adapters = await request('/api/v1/admin/adapters', admin);
  const enabled = adapters.adapters.filter((adapter) => adapter.enabled);
  assert.equal(enabled.length, 2);
  assert.ok(enabled.every((adapter) => adapter.type === 'simulator' && adapter.host === 'fixsimulator'));

  const events = await watchEvents(admin);
  try {
  await events.waitFor('event: ready');

  const run = randomUUID();
  async function broker(suffix) {
    const { broker: created } = await request('/api/v1/admin/brokers', admin, 'POST', {
      code: `SMOKE-${run}-${suffix}`, name: `Local smoke ${suffix}`,
    }, 201);
    await request(`/api/v1/admin/brokers/${created.id}/accounts`, admin, 'POST', {
      code: 'SMOKE', name: 'Simulator account', quik_firm: 'SIM', trading_account: 'SMOKE',
    }, 201);
    return request(`/api/v1/admin/brokers/${created.id}/tokens`, admin, 'POST', {
      name: 'Smoke test', quik_firm: 'SIM', expires_in_days: 30,
    }, 201);
  }
  const first = await broker('A');
  const second = await broker('B');
  const order = {
    account_id: 'SMOKE', client_code: 'SMOKE', symbol: 'SBER', board: 'TQBR',
    side: 'BUY', order_type: 'LIMIT', time_in_force: 'DAY', quantity: '1', price: '100',
    client_order_id: `SMOKE-${run}-fill`,
  };
  const created = await request('/api/v1/orders', first.token, 'POST', order, 201);
  const repeated = await request('/api/v1/orders', first.token, 'POST', order);
  assert.equal(repeated.order.id, created.order.id);
  assert.equal(repeated.idempotent, true);
  await request(`/api/v1/orders/${created.order.id}`, second.token, 'GET', undefined, 404);
  const filled = await waitStatus(created.order.id, first.token, 'FILLED');
  assert.equal(Number(filled.order.filled_quantity), 1);
  assert.equal(filled.trades.length, 1);
  await events.waitFor('order.updated');
  console.log('PASS create / idempotency / fill / trade / broker isolation');
  console.log('PASS realtime SSE over native mTLS gRPC');

  const cancel = await request('/api/v1/orders', first.token, 'POST', {
    ...order, client_order_id: `SMOKE-${run}-cancel`,
  }, 201);
  await waitStatus(cancel.order.id, first.token, 'ACKNOWLEDGED');
  await request(`/api/v1/orders/${cancel.order.id}/cancel`, first.token, 'POST');
  await waitStatus(cancel.order.id, first.token, 'CANCELLED');
  await delay(3500);
  const cancelled = await request(`/api/v1/orders/${cancel.order.id}`, first.token);
  assert.equal(cancelled.order.status, 'CANCELLED');
  assert.equal(cancelled.trades.length, 0);
  console.log('PASS cancel stays cancelled after simulator fill deadline');

  const rejected = await request('/api/v1/orders', first.token, 'POST', {
    ...order, symbol: 'REJECT', client_order_id: `SMOKE-${run}-reject`,
  }, 201);
  await waitStatus(rejected.order.id, first.token, 'REJECTED');
  console.log('PASS FIX rejection');

  const batch = await Promise.all(Array.from({ length: 8 }, (_, index) =>
    request('/api/v1/orders', first.token, 'POST', {
      ...order, client_order_id: `SMOKE-${run}-parallel-${index}`,
    }, 201)));
  const completed = await Promise.all(batch.map(({ order: item }) => waitStatus(item.id, first.token, 'FILLED')));
  assert.equal(new Set(completed.map((item) => item.order.id)).size, 8);
  assert.equal(new Set(completed.map((item) => item.order.adapter_id)).size, 2);
  assert.ok(completed.every((item) => item.trades.length === 1));
  console.log('PASS concurrent orders balanced across two FIX adapters');

  await request('/api/v1/admin/adapters/adapter-1/disable', admin, 'POST');
  try {
    await request('/api/v1/admin/adapters/adapter-2/disable', admin, 'POST', undefined, 409);
    const fallback = await request('/api/v1/orders', first.token, 'POST', {
      ...order, client_order_id: `SMOKE-${run}-fallback`,
    }, 201);
    const result = await waitStatus(fallback.order.id, first.token, 'FILLED');
    assert.equal(result.order.adapter_id, 'adapter-2');
    console.log('PASS routing with one adapter disabled / last-adapter protection');
  } finally {
    await request('/api/v1/admin/adapters/adapter-1/enable', admin, 'POST');
  }
  for (const token of [first, second]) {
    await request(`/api/v1/admin/tokens/${token.id}/revoke`, admin, 'POST');
    await request('/api/v1/orders', token.token, 'GET', undefined, 401);
  }
  console.log('PASS token revocation; local smoke complete');
  } finally {
    await events.close();
  }
}

main().catch((error) => { console.error(error.message); process.exitCode = 1; });
