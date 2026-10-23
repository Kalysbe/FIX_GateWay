import type { SessionUser } from "./types/admin";

const API_URL = import.meta.env.VITE_API_URL || "";
const TOKEN_KEY = "fg_token";
const USER_KEY = "fg_user";
// Invalidate legacy persistent credentials after upgrading session security.
localStorage.removeItem(TOKEN_KEY);
localStorage.removeItem(USER_KEY);

let redirectingToLogin = false;

export class ApiError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export function getToken() {
  return sessionStorage.getItem(TOKEN_KEY) || "";
}

export function setToken(token: string) {
  redirectingToLogin = false;
  sessionStorage.removeItem(USER_KEY);
  sessionStorage.setItem(TOKEN_KEY, token);
}

export function setSessionUser(user: SessionUser) {
  sessionStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function getSessionUser(): SessionUser | null {
  try {
    const raw = sessionStorage.getItem(USER_KEY);
    if (raw) return JSON.parse(raw) as SessionUser;
  } catch {
    /* ignore */
  }
  const payload = decodeJwtPayload(getToken());
  if (!payload) return null;
  return { email: payload.email, role: payload.role };
}

export function clearToken() {
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(USER_KEY);
}

type JwtPayload = { exp?: number; email?: string; role?: string; sub?: string };

function decodeJwtPayload(token: string): JwtPayload | null {
  const parts = token.split(".");
  if (parts.length < 2) return null;
  try {
    const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
    return JSON.parse(atob(padded));
  } catch {
    return null;
  }
}

export function isSessionValid(): boolean {
  const token = getToken();
  if (!token) return false;
  const payload = decodeJwtPayload(token);
  if (!payload) return false;
  if (typeof payload.exp !== "number") return false;
  return payload.exp * 1000 > Date.now() + 1000;
}

export function redirectToLogin(expectedToken?: string) {
  // A request or expiry timer from an earlier session cannot log out a new one.
  if (expectedToken !== undefined && expectedToken !== getToken()) return;
  if (redirectingToLogin) return;
  redirectingToLogin = true;
  clearToken();
  if (window.location.pathname !== "/login") {
    window.location.assign("/login");
  }
}

export function watchSessionExpiry(): () => void {
  const token = getToken();
  const payload = decodeJwtPayload(token);
  if (typeof payload?.exp !== "number") return () => {};
  const ms = payload.exp * 1000 - Date.now();
  if (ms <= 0) {
    redirectToLogin(token);
    return () => {};
  }
  const id = window.setTimeout(() => redirectToLogin(token), ms);
  return () => window.clearTimeout(id);
}

export async function consumeAdminEvents(
  signal: AbortSignal,
  onReady: () => void,
  onMessage: (message: { type?: string; data?: unknown }) => void,
): Promise<void> {
  const token = getToken();
  const response = await fetch(`${API_URL}/api/v1/admin/events/stream`, {
    headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" },
    cache: "no-store", signal,
  });
  if (response.status === 401) redirectToLogin(token);
  if (!response.ok || !response.body) throw new Error("Realtime stream unavailable");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "";
  let data: string[] = [];
  let frameSize = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) throw new Error("Realtime stream closed");
      buffer += decoder.decode(value, { stream: true });
      if (buffer.length + frameSize > 8 * 1024 * 1024) throw new Error("Realtime frame too large");
      let index: number;
      while ((index = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, index).replace(/\r$/, "");
        buffer = buffer.slice(index + 1);
        frameSize += line.length;
        if (!line) {
          if (event === "ready") onReady();
          else if (data.length) onMessage(JSON.parse(data.join("\n")));
          event = ""; data = []; frameSize = 0;
        } else if (line.startsWith("event:")) event = line.slice(6).trimStart();
        else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
      }
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
function readErrorBody(body: Record<string, unknown>, fallback: string) {
  const err = body.error ?? body.message;
  return typeof err === "string" && err ? err : fallback;
}

export async function api<T>(path: string, init: RequestInit = {}, parse: (text: string) => T = JSON.parse): Promise<T> {
  return (await apiWithHeaders(path, init, parse)).data;
}

/** Same as api(), and also returns response headers (e.g. X-Has-More paging). */
export async function apiWithHeaders<T>(path: string, init: RequestInit = {}, parse: (text: string) => T = JSON.parse): Promise<{ data: T; headers: Headers }> {
  const headers = new Headers(init.headers);
  const hasBody = init.body != null && init.body !== "";
  if (hasBody && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_URL}${path}`, { ...init, headers });
  if (!res.ok) {
    if (res.status === 401 && path !== "/api/v1/admin/login") {
      redirectToLogin(token);
    }
    const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    throw new ApiError(readErrorBody(body, res.statusText), res.status, typeof body.code === "string" ? body.code : undefined);
  }
  if (res.status === 204) return { data: undefined as T, headers: res.headers };
  const text = await res.text();
  if (!text) return { data: undefined as T, headers: res.headers };
  return { data: parse(text), headers: res.headers };
}

export async function apiDownload(path: string): Promise<{ blob: Blob; filename: string; json?: { job?: { id?: string } } }> {
  const headers = new Headers();
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_URL}${path}`, { headers });
  if (!res.ok) {
    if (res.status === 401) redirectToLogin(token);
    const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    throw new ApiError(readErrorBody(body, res.statusText), res.status, typeof body.code === "string" ? body.code : undefined);
  }
  const ctype = res.headers.get("Content-Type") || "";
  if (ctype.includes("application/json") && res.status === 202) {
    return { blob: new Blob(), filename: "", json: (await res.json()) as { job?: { id?: string } } };
  }
  const disp = res.headers.get("Content-Disposition") || "";
  const m = /filename=([^;]+)/.exec(disp);
  const filename = m ? m[1] : "export";
  return { blob: await res.blob(), filename };
}
