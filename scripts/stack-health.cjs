'use strict';

// Compose v2 emits either a JSON array or one JSON object per line.
function parseContainers(raw) {
  const value = raw.trim();
  if (!value) return [];
  return value.startsWith('[')
    ? JSON.parse(value)
    : value.split(/\r?\n/).filter(Boolean).map((line) => JSON.parse(line));
}

function containersHealthy(raw, services) {
  if (services.length === 0) return false;
  let containers;
  try { containers = parseContainers(raw); } catch { return false; }
  return services.every((service) => {
    const instances = containers.filter((container) => container.Service === service);
    if (instances.length === 0) return false;
    return instances.every((container) => {
      // The certificate generator is intentionally a successful one-shot job.
      if (service === 'devcerts') {
        return container.State === 'exited' && container.ExitCode === 0;
      }
      return container.State === 'running'
        && (!container.Health || container.Health === 'healthy');
    });
  });
}

module.exports = { parseContainers, containersHealthy };
