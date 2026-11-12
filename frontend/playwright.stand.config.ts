import { defineConfig, devices } from "@playwright/test";

// Administrator scenarios against a running local stand (deploy/docker-compose.local.yml):
// the real gateway, PostgreSQL and FIX simulator, no mocks. Start the stand first.
// This build of the console is served by `vite preview` and forwards /api to the
// stand's gateway (GATEWAY_URL, default localhost:28080); STAND_URL points at an
// already running console instead. STAND_USERNAME / STAND_PASSWORD override the
// stand's documented credentials.
const PORT = 5180;
const external = process.env.STAND_URL;

export default defineConfig({
  testDir: "e2e-stand",
  workers: 1,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: external ?? `http://127.0.0.1:${PORT}`,
    locale: "ru-RU",
    timezoneId: "Asia/Bishkek",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: external ? undefined : {
    command: `npm run build && npx vite preview --host 127.0.0.1 --port ${PORT} --strictPort`,
    url: `http://127.0.0.1:${PORT}/login`,
    env: { GATEWAY_URL: process.env.GATEWAY_URL ?? "http://localhost:28080" },
    reuseExistingServer: false,
    timeout: 180_000,
  },
});
