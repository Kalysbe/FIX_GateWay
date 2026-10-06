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
