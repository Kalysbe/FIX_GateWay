const API_URL = import.meta.env.VITE_API_URL || "";

let redirectingToLogin = false;

export function getToken() {
  return localStorage.getItem("fg_token") || "";
}

export function setToken(token: string) {
  localStorage.setItem("fg_token", token);
}

export function clearToken() {
  localStorage.removeItem("fg_token");
}

function decodeJwtPayload(token: string): { exp?: number } | null {
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
  if (typeof payload.exp !== "number") return true;
  return payload.exp * 1000 > Date.now() + 1000;
}

export function redirectToLogin() {
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
    redirectToLogin();
    return () => {};
  }
  const id = window.setTimeout(redirectToLogin, ms);
  return () => window.clearTimeout(id);
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_URL}${path}`, { ...init, headers });
  if (!res.ok) {
    if (res.status === 401 && path !== "/api/v1/admin/login") {
      redirectToLogin();
    }
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || res.statusText);
  }
  return res.json();
}

export async function apiDownload(path: string): Promise<{ blob: Blob; filename: string; json?: any }> {
  const headers = new Headers();
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_URL}${path}`, { headers });
  if (!res.ok) {
    if (res.status === 401) redirectToLogin();
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || res.statusText);
  }
  const ctype = res.headers.get("Content-Type") || "";
  if (ctype.includes("application/json") && res.status === 202) {
    return { blob: new Blob(), filename: "", json: await res.json() };
  }
  const disp = res.headers.get("Content-Disposition") || "";
  const m = /filename=([^;]+)/.exec(disp);
  const filename = m ? m[1] : "export";
  return { blob: await res.blob(), filename };
}
