'use strict';

/**
 * Called by Task Scheduler on logon / periodically.
 * Resurrects the PM2 daemon and starts fix-gateway if it is not online.
 */
const { execFileSync, spawn, execSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const ROOT = path.resolve(__dirname, '..');
const NODE_DIR = process.env.GATEWAY_NODE_DIR || path.dirname(process.execPath);
const NODE_EXE = path.join(NODE_DIR, 'node.exe');
const PM2_CLI = process.env.GATEWAY_PM2_CLI || path.join(NODE_DIR, 'node_modules', 'pm2', 'bin', 'pm2');
const DOCKER_DIR = 'C:\\Program Files\\Docker\\Docker\\resources\\bin';
const DOCKER_DESKTOP = 'C:\\Program Files\\Docker\\Docker\\Docker Desktop.exe';
const PM2_HOME = path.join(process.env.USERPROFILE || '', '.pm2');
const ECOSYSTEM = path.join(ROOT, 'ecosystem.config.cjs');

process.env.PATH = [NODE_DIR, DOCKER_DIR, process.env.PATH || ''].join(';');
process.env.PM2_HOME = process.env.PM2_HOME || PM2_HOME;

function log(msg) {
  console.log(`[${new Date().toISOString()}] ${msg}`);
}

function runPm2(args, inherit = false) {
  return execFileSync(NODE_EXE, [PM2_CLI, ...args], {
    cwd: ROOT,
    encoding: 'utf8',
    timeout: 60_000,
    windowsHide: true,
    stdio: inherit ? 'inherit' : ['ignore', 'pipe', 'pipe'],
  });
}

function gatewayOnline() {
  try {
    const raw = runPm2(['jlist']);
    const list = JSON.parse(raw || '[]');
    return list.some(
      (p) => p.name === 'fix-gateway' && p.pm2_env && p.pm2_env.status === 'online',
    );
  } catch {
    return false;
  }
}

function startDockerDesktop() {
  try {
    execFileSync('net', ['start', 'com.docker.service'], {
      stdio: 'ignore',
      timeout: 30_000,
      windowsHide: true,
    });
  } catch {
    // best-effort without admin
  }
  if (!fs.existsSync(DOCKER_DESKTOP)) return;
  try {
    const out = execSync(
      'tasklist /FI "IMAGENAME eq Docker Desktop.exe" /NH',
      { encoding: 'utf8', timeout: 8_000, windowsHide: true },
    );
    if (/Docker Desktop\.exe/i.test(out)) return;
  } catch {
    // continue and try to launch
  }
  log('Launching Docker Desktop');
  const child = spawn(DOCKER_DESKTOP, [], {
    detached: true,
    stdio: 'ignore',
    windowsHide: true,
  });
  child.unref();
}

function main() {
  if (!fs.existsSync(NODE_EXE) || !fs.existsSync(PM2_CLI)) {
    throw new Error(`PM2/node not found under ${NODE_DIR}`);
  }
  startDockerDesktop();

  if (gatewayOnline()) {
    log('fix-gateway already online');
    return;
  }

  log('fix-gateway not online, resurrecting PM2');
  try {
    runPm2(['resurrect'], true);
  } catch (err) {
    log(`pm2 resurrect: ${err.message || err}`);
  }

  if (gatewayOnline()) {
    log('fix-gateway online after resurrect');
    return;
  }

  log('Restarting fix-gateway');
  try {
    runPm2(['restart', 'fix-gateway'], true);
  } catch (err) {
    log(`pm2 restart: ${err.message || err}`);
  }

  if (gatewayOnline()) {
    log('fix-gateway online after restart');
    return;
  }

  log('Starting ecosystem.config.cjs');
  runPm2(['start', ECOSYSTEM], true);
  try {
    runPm2(['save'], true);
  } catch (err) {
    log(`pm2 save: ${err.message || err}`);
  }
  log('Boot complete');
}

try {
  main();
} catch (err) {
  console.error(err);
  process.exit(1);
}
