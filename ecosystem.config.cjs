'use strict';

const path = require('path');

module.exports = {
  apps: [
    {
      name: 'fix-gateway',
      cwd: __dirname,
      script: path.join(__dirname, 'scripts', 'pm2-supervisor.cjs'),
      interpreter: 'node',
      autorestart: true,
      watch: false,
      min_uptime: 5000,
      max_restarts: 999999,
      restart_delay: 3000,
      exp_backoff_restart_delay: 100,
      kill_timeout: 8000,
      error_file: path.join(__dirname, 'logs', 'pm2-error.log'),
      out_file: path.join(__dirname, 'logs', 'pm2-out.log'),
      merge_logs: true,
      time: true,
    },
  ],
};
