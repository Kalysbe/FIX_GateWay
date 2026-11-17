/** The queries an admin stream event makes stale; unknown events refresh nothing. */
export function refreshKeysFor(type: string | undefined): string[][] {
  switch (type) {
    case "notification.new":
      return [["notifications"]];
    case "order.updated":
    case "trade.created":
      // Quotes and an instrument's order book are today's active Orders rows too.
      return [["admin-orders"], ["admin-quotes"], ["admin-market-data", "details"], ["order"], ["order-trace"], ["admin-trades"], ["admin-trade"], ["dashboard"]];
    case "moneylimits.updated":
    case "depolimits.updated":
      return [["admin-positions"]];
    case "marketdata.tick":
      return [["admin-market-data"]];
    case "adapter.status_changed":
      return [["adapters"], ["dashboard"]];
    case "obs.event":
      return [["dashboard"], ["logs"], ["broker-logs"], ["broker-errors"]];
    default:
      return [];
  }
}

// Fixed windows keep refreshes bounded even when events never stop arriving.
export function eventRefresh(flush: (keys: string[][]) => void, interval = 2000) {
  const pending = new Map<string, string[]>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    add(keys: string[][]) {
      for (const key of keys) pending.set(JSON.stringify(key), key);
      if (timer !== undefined) return;
      timer = setTimeout(() => {
        timer = undefined;
        const keys = [...pending.values()];
        pending.clear();
        flush(keys);
      }, interval);
    },
    stop() { clearTimeout(timer); timer = undefined; pending.clear(); },
  };
}
