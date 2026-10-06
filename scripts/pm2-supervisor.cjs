'use strict';

/**
 * Keeps the Docker Compose stack running. PM2 restarts this process if it dies;
 * this loop restarts Docker Desktop / compose if they drop.
 */
const { spawn, execFileSync } = require('child_process');
const http = require('http');
const path = require('path');
const fs = require('fs');
const { parseContainers, containersHealthy } = require('./stack-health.cjs');

const ROOT = path.resolve(__dirname, '..');
const DOCKER_BIN = 'C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe';
const DOCKER_DESKTOP = 'C:\\Program Files\\Docker\\Docker\\Docker Desktop.exe';
const HEALTH_URL = process.env.GATEWAY_HEALTH_URL || 'http://127.0.0.1:8080/health';
const HEALTHY_INTERVAL_MS = 15_000;
const UNHEALTHY_INTERVAL_MS = 5_000;
const COMPOSE_COOLDOWN_MS = 20_000;
const DOCKER_WAIT_MS = 5_000;
const DOCKER_WAIT_ATTEMPTS = 60;

process.env.PATH = [
  path.dirname(DOCKER_BIN),
  process.env.PATH || '',
].join(';');

let stopping = false;
let lastComposeAt = 0;
let unhealthyServices = [];

function log(msg) {
  const ts = new Date().toISOString();
  console.log(`[${ts}] ${msg}`);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function dockerReady() {
  try {
    execFileSync(DOCKER_BIN, ['info'], {
      stdio: 'ignore',
      timeout: 15_000,
      windowsHide: true,
    });
    return true;
  } catch {
    return false;
  }
}

function dockerDesktopRunning() {
  try {
    const out = execFileSync(
      'tasklist',
      ['/FI', 'IMAGENAME eq Docker Desktop.exe', '/NH'],
      { encoding: 'utf8', timeout: 8_000, windowsHide: true },
    );
    return /Docker Desktop\.exe/i.test(out);
  } catch {
    return false;
  }
}

function tryStartDockerService() {
  try {
    execFileSync('net', ['start', 'com.docker.service'], {
      stdio: 'ignore',
      timeout: 30_000,
      windowsHide: true,
    });
    log('Started Docker Desktop Service');
  } catch {
    // Changing the service to Automatic needs admin; starting it is best-effort.
  }
}

function startDockerDesktop() {
  tryStartDockerService();
  if (!fs.existsSync(DOCKER_DESKTOP)) {
    throw new Error(`Docker Desktop not found: ${DOCKER_DESKTOP}`);
  }
  if (dockerDesktopRunning()) return;
  log('Starting Docker Desktop...');
  const child = spawn(DOCKER_DESKTOP, [], {
    detached: true,
    stdio: 'ignore',
    windowsHide: true,
  });
  child.unref();
}

async function ensureDocker() {
  if (dockerReady()) return;
  startDockerDesktop();
  for (let i = 1; i <= DOCKER_WAIT_ATTEMPTS; i += 1) {
    if (stopping) return;
    log(`Waiting for Docker engine (${i}/${DOCKER_WAIT_ATTEMPTS})...`);
    await sleep(DOCKER_WAIT_MS);
    if (dockerReady()) {
      log('Docker engine is ready');
      return;
    }
  }
  throw new Error('Docker engine did not become ready');
}

function composeUp() {
  const now = Date.now();
  if (now - lastComposeAt < COMPOSE_COOLDOWN_MS) return;
  lastComposeAt = now;
  log('docker compose up -d');
  execFileSync(DOCKER_BIN, ['compose', 'up', '-d', '--remove-orphans'], {
    cwd: ROOT,
    stdio: 'inherit',
    timeout: 180_000,
    windowsHide: true,
  });
  if (unhealthyServices.length) {
    // Compose up does not restart a running container whose health check failed.
    execFileSync(DOCKER_BIN, ['compose', 'restart', ...unhealthyServices], {
      cwd: ROOT, stdio: 'inherit', timeout: 120_000, windowsHide: true,
    });
  }
}

function httpOk(url) {
  return new Promise((resolve) => {
    const req = http.get(url, { timeout: 4_000 }, (res) => {
      res.resume();
      resolve(res.statusCode >= 200 && res.statusCode < 300);
    });
    req.on('error', () => resolve(false));
    req.on('timeout', () => {
      req.destroy();
      resolve(false);
    });
  });
}

async function stackHealthy() {
  unhealthyServices = [];
  try {
    const services = execFileSync(DOCKER_BIN, ['compose', 'config', '--services'], {
      cwd: ROOT, encoding: 'utf8', timeout: 15_000, windowsHide: true,
    }).trim().split(/\r?\n/).filter(Boolean);
    const raw = execFileSync(DOCKER_BIN, ['compose', 'ps', '--all', '--format', 'json'], {
      cwd: ROOT, encoding: 'utf8', timeout: 15_000, windowsHide: true,
    });
    unhealthyServices = [...new Set(parseContainers(raw)
      .filter((container) => container.State === 'running' && container.Health === 'unhealthy')
      .map((container) => container.Service))];
    if (!containersHealthy(raw, services)) return false;
    const healthy = await httpOk(HEALTH_URL);
    if (!healthy) unhealthyServices = ['gateway'];
    return healthy;
  } catch {
    return false;
  }
}

async function loop() {
  log(`Supervisor started, cwd=${ROOT}`);
  while (!stopping) {
    try {
      await ensureDocker();
      if (stopping) break;
      const healthy = await stackHealthy();
      if (!healthy) {
        log('Compose services or gateway health check failed, bringing stack up');
        composeUp();
        await sleep(UNHEALTHY_INTERVAL_MS);
        continue;
      }
      await sleep(HEALTHY_INTERVAL_MS);
    } catch (err) {
      log(`Supervisor error: ${err && err.message ? err.message : err}`);
      await sleep(UNHEALTHY_INTERVAL_MS);
    }
  }
  log('Supervisor stopped');
}

function onStop(signal) {
  if (stopping) return;
  stopping = true;
  log(`Received ${signal}, exiting (containers stay up)`);
  setTimeout(() => process.exit(0), 500).unref();
}

process.on('SIGINT', () => onStop('SIGINT'));
process.on('SIGTERM', () => onStop('SIGTERM'));

loop().catch((err) => {
  console.error(err);
  process.exit(1);
});
