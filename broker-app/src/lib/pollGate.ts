// One outstanding poll per broker; transient errors back off instead of piling up.
export class PollGate {
  private running = new Set<string>();
  private failures = new Map<string, { count: number; next: number }>();

  begin(key: string, now = Date.now()): boolean {
    if (this.running.has(key) || (this.failures.get(key)?.next || 0) > now) return false;
    this.running.add(key);
    return true;
  }

  success(key: string): void { this.failures.delete(key); }

  fail(key: string, retryAfterMs = 0, now = Date.now(), random = Math.random()): void {
    const count = Math.min(6, (this.failures.get(key)?.count || 0) + 1);
    const delay = Math.min(60000, 1000 * 2 ** count) * (1 + random * 0.25);
    this.failures.set(key, { count, next: now + Math.max(retryAfterMs, delay) });
  }

  finish(key: string): void { this.running.delete(key); }
}
