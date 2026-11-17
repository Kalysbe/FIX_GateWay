import type { Page, Request, Route } from "@playwright/test";

export type Role = "SUPER_ADMIN" | "ADMIN" | "OPERATOR" | "VIEWER";
type Handler = (request: Request, match: RegExpMatchArray) => unknown | Promise<unknown>;
type Reply = { status?: number; body?: unknown; headers?: Record<string, string> };

/** A reply with a status other than 200, or with headers. */
export const reply = (r: Reply) => ({ __reply: r });

/** An unsigned session token: the console only reads its expiry and role. */
export function sessionToken(role: Role, username = "admin", ttlSeconds = 3600) {
  const part = (v: object) => Buffer.from(JSON.stringify(v)).toString("base64url");
  return `${part({ alg: "none", typ: "JWT" })}.${part({ sub: "1", username, role, exp: Math.floor(Date.now() / 1000) + ttlSeconds })}.e2e`;
}

/** Opens the console already signed in with the given role. */
export async function signIn(page: Page, role: Role = "ADMIN", ttlSeconds = 3600) {
  const token = sessionToken(role, "admin", ttlSeconds);
  await page.addInitScript(([t, r]) => {
    sessionStorage.setItem("fg_token", t);
    sessionStorage.setItem("fg_user", JSON.stringify({ username: "admin", role: r }));
  }, [token, role] as const);
}

/**
 * Answers /api requests from the routes given; anything else gets 404 so a
 * page shows its error state instead of the test reaching a real server.
 * Returns the requests the page sent, for assertions on what was submitted.
 */
export async function mockApi(page: Page, routes: Record<string, Handler>) {
  const sent: { method: string; path: string; body: unknown }[] = [];
  const table = Object.entries({ ...defaults, ...routes }).map(([key, handler]) => {
    const [method, pattern] = key.split(" ");
    return { method, re: new RegExp(`^${pattern}$`), handler };
  });
  await page.route(url => url.pathname.startsWith("/api/") || url.pathname === "/health", async (route: Route) => {
    const request = route.request();
    const url = new URL(request.url());
    const body = request.postData() ? safeJSON(request.postData()!) : undefined;
    sent.push({ method: request.method(), path: url.pathname + url.search, body });
    for (const { method, re, handler } of table) {
      const match = url.pathname.match(re);
      if (match && method === request.method()) {
        const result = await handler(request, match);
        const r: Reply = result && typeof result === "object" && "__reply" in result ? (result as { __reply: Reply }).__reply : { body: result };
        return route.fulfill({ status: r.status ?? 200, headers: { "Content-Type": "application/json", ...r.headers }, body: r.body === undefined ? "" : JSON.stringify(r.body) });
      }
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ error: `no e2e mock for ${request.method()} ${url.pathname}` }) });
  });
  return sent;
}

const safeJSON = (text: string) => { try { return JSON.parse(text); } catch { return text; } };

// What the shell asks for on every page.
const defaults: Record<string, Handler> = {
  "GET /api/v1/admin/events/stream": () => reply({ status: 503, body: { error: "stream disabled in e2e" } }),
  "GET /api/v1/admin/notifications": () => ({ notifications: [], unread: 0 }),
  "GET /api/v1/admin/adapters": () => ({ adapters: [
    { id: "quik-fix-1", name: "quik-fix-1", type: "arqa", enabled: true, status: "ONLINE", active_orders: 0 },
    { id: "quik-fix-2", name: "quik-fix-2", type: "arqa", enabled: true, status: "ONLINE", active_orders: 0 },
  ] }),
};

export const broker = (id: number, name: string, extra: Record<string, unknown> = {}) => ({
  id, code: `B${id}`, name, status: "ACTIVE", created_at: "2026-11-01T09:00:00Z", connected: false, ...extra,
});
