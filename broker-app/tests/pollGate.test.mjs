import test from 'node:test';
import assert from 'node:assert/strict';
import { PollGate } from '../src/lib/pollGate.ts';

test('slow polls cannot overlap, different brokers remain independent', () => {
  const gate = new PollGate();
  assert.equal(gate.begin('a', 0), true);
  assert.equal(gate.begin('a', 100000), false);
  assert.equal(gate.begin('b', 0), true);
  gate.finish('a');
  assert.equal(gate.begin('a', 100000), true);
});

test('backoff honors Retry-After and resets after recovery', () => {
  const gate = new PollGate();
  gate.fail('a', 10000, 0, 0);
  assert.equal(gate.begin('a', 9999), false);
  assert.equal(gate.begin('a', 10000), true);
  gate.finish('a');
  gate.fail('a', 0, 10000, 0);
  assert.equal(gate.begin('a', 13999), false);
  gate.success('a');
  assert.equal(gate.begin('a', 10001), true);
});
