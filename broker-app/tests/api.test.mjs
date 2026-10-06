import test from 'node:test';
import assert from 'node:assert/strict';
import { ApiError, getOrder, cancelOrder, listOrders } from '../src/lib/api.ts';

test('mock/client IDs are rejected without sending requests', async () => {
  const original = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => { calls++; throw new Error('unexpected request'); };
  try {
    await assert.rejects(getOrder('http://gateway', 'token', 'VENUE-LOAD-123'), e => e instanceof ApiError && e.status === 400);
    await assert.rejects(cancelOrder('http://gateway', 'token', 'LOAD-123'), e => e instanceof ApiError && e.status === 400);
    assert.equal(calls, 0);
  } finally { globalThis.fetch = original; }
});

test('overload Retry-After is available for poll backoff', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify({error:'busy'}), {status:429,headers:{'Retry-After':'7'}});
  try {
    await assert.rejects(listOrders('http://gateway', 'token'), e => e instanceof ApiError && e.retryAfterMs === 7000);
  } finally { globalThis.fetch = original; }
});
