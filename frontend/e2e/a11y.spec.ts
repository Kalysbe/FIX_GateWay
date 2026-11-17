import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { broker, mockApi, signIn } from "./mock-api";

// WCAG 2.1 A/AA checks of the main pages and dialogs, in both themes.
const order = (n: number) => ({ OrderNum: 7000 + n, OrderDateTime: `2026-11-06 10:0${n}:00`, TradeDate: "2026-11-06", ClassCode: "LIST_GBA", SecCode: `GBA0${n}`, Operation: n % 2 ? "Купля" : "Продажа", State: "Активна", Price: 99 + n, Qty: 10, Balance: 10, FirmId: "B1" });
const routes = {
  "GET /api/v1/admin/dashboard": () => ({ system: { api: "ONLINE", database: "ONLINE", queue: "DEGRADED" }, adapters: [{ id: "quik-fix-1", name: "quik-fix-1", status: "ONLINE", enabled: true, weight: 1, active_orders: 2, processed_orders: 10 }], stats: { total: 10, filled: 4, rejected: 1, active: 5 }, brokers: 1, adapters_available: 1, observability: { requests_today: 120, orders_today: 4, successful: 118, failed: 2, success_rate: 98.3, avg_latency_ms: 20, p95_latency_ms: 60, p99_latency_ms: 90, errors_today: 2, critical_today: 0, auth_failures_today: 1, active_brokers: 1 } }),
  "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа Капитал", { connected: true, last_activity: "2026-11-06T10:00:00Z" }), broker(2, "Бета", { status: "BLOCKED" })] }),
  "GET /api/v1/admin/orders": () => [1, 2, 3].map(order),
  "GET /api/v1/admin/notifications": () => ({ notifications: [{ id: "n1", severity: "CRITICAL", event_type: "error", subtype: "FIX_SESSION_DOWN", title: "Нет FIX-сессии", message: "quik-fix-1 недоступен", count: 3, last_seen_at: "2026-11-06T10:00:00Z", read_at: null }], unread: 1, total: 1, limit: 20, page: 1, has_more: false }),
  "GET /api/v1/admin/logs": () => ({ logs: [], next_cursor: null }),
  "GET /api/v1/admin/rate-limits": () => ({ policies: [{ scope: "broker", subject: "1", operation: "SubmitOrder", rate: 50, burst: 50, concurrent: 8, enabled: true }], usage: [], operations: ["*", "SubmitOrder", "Trading", "System"] }),
  "GET /api/v1/admin/trading-limits": () => ({ policies: [{ broker_id: 0, paused: false, max_quantity: null, max_open_orders: null, revision: 1 }] }),
  "GET /api/v1/admin/trading-limits/history": () => ({ history: [], next_before_id: null }),
};

async function audit(page: Page, label: string) {
  // Let entry animations finish: axe measures the colours on screen.
  await page.waitForTimeout(450);
  const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const report = result.violations.map(v => `${v.id} (${v.impact}): ${v.nodes.slice(0, 3).map(n => n.target.join(" ")).join(" | ")}`);
  expect(report, `${label}: ${report.join("\n")}`).toEqual([]);
}

for (const theme of ["light", "dark"] as const) {
  test.describe(`доступность · ${theme === "light" ? "светлая" : "тёмная"} тема`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript(t => { try { localStorage.setItem("fixgw-theme", t); } catch { /* blocked */ } }, theme);
    });

    test("страница входа", async ({ page }) => {
      await mockApi(page, {});
      await page.goto("/login");
      await audit(page, "login");
      await page.getByRole("button", { name: "Войти в консоль" }).click();
      await audit(page, "login with errors");
    });

    for (const path of ["/", "/brokers", "/orders", "/notifications", "/logs", "/rate-limits"]) {
      test(`страница ${path}`, async ({ page }) => {
        await signIn(page, "ADMIN");
        await mockApi(page, routes);
        await page.goto(path);
        await expect(page.locator("#main-content h1").first()).toBeVisible();
        await audit(page, path);
      });
    }

    test("диалоги: форма с ошибками и подтверждение", async ({ page }) => {
      await signIn(page, "ADMIN");
      await mockApi(page, routes);
      await page.goto("/brokers");
      await page.getByRole("button", { name: "Создать брокера" }).click();
      await page.getByRole("dialog").getByRole("button", { name: "Создать" }).click();
      await audit(page, "create broker dialog with errors");
      await page.keyboard.press("Escape");
      await page.getByRole("button", { name: "Заблокировать" }).click();
      await audit(page, "confirm dialog");
    });

    test("панель строки", async ({ page }) => {
      await signIn(page, "ADMIN");
      await mockApi(page, routes);
      await page.goto("/orders");
      await page.getByRole("region", { name: "Данные: Заявки" }).getByRole("row").nth(1).getByRole("cell").nth(2).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await audit(page, "row drawer");
    });
  });
}
