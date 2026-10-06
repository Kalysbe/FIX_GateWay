'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const { containersHealthy } = require('./stack-health.cjs');

const services = ['gateway', 'frontend', 'fixsimulator', 'devcerts'];
const healthy = [
  { Service: 'gateway', State: 'running', Health: 'healthy' },
  { Service: 'frontend', State: 'running', Health: '' },
  { Service: 'fixsimulator', State: 'running', Health: '' },
  { Service: 'devcerts', State: 'exited', ExitCode: 0 },
];

test('supports Compose JSON array and JSON lines, including completed PKI job', () => {
  assert.equal(containersHealthy(JSON.stringify(healthy), services), true);
  assert.equal(containersHealthy(healthy.map((item) => JSON.stringify(item)).join('\n'), services), true);
});

test('a live gateway does not hide a stopped or missing companion service', () => {
  for (const service of ['frontend', 'fixsimulator']) {
    assert.equal(containersHealthy(JSON.stringify(healthy.filter((item) => item.Service !== service)), services), false);
    assert.equal(containersHealthy(JSON.stringify(healthy.map((item) => item.Service === service
      ? { ...item, State: 'exited', ExitCode: 1 } : item)), services), false);
  }
});

test('rejects unhealthy containers, failed certificates and malformed output', () => {
  assert.equal(containersHealthy(JSON.stringify(healthy.map((item) => item.Service === 'gateway'
    ? { ...item, Health: 'unhealthy' } : item)), services), false);
  assert.equal(containersHealthy(JSON.stringify(healthy.map((item) => item.Service === 'devcerts'
    ? { ...item, ExitCode: 1 } : item)), services), false);
  for (const raw of ['', 'invalid', '[]']) {
    assert.equal(containersHealthy(raw, services), false);
  }
});
