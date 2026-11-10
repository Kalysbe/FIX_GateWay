import { expect, test } from "@playwright/test";
import { mockApi, signIn } from "./mock-api";

const note = (id: string, extra: Record<string, unknown> = {}) => ({ id, severity: "ERROR", event_type: "error", subtype: "FIX_SESSION_DOWN", error_code: "FIX_SESSION_DOWN", title: `Событие ${id}`, message: "Сессия FIX недоступна", count: 1, last_seen_at: "2026-11-06T10:00:00Z", read_at: null, ...extra });

test.beforeEach(async ({ page }) => signIn(page, "ADMIN"));

test("уведомления: выбор непрочитанных и отметка одним действием", async ({ page }) => {
  const notes = [note("n1"), note("n2", { severity: "CRITICAL" }), note("n3", { read_at: "2026-11-06T10:05:00Z" })];
  const sent = await mockApi(page, {
    "GET /api/v1/admin/notifications": request => {
      const unread = new URL(request.url()).searchParams.get("unread") === "1";
      const list = unread ? notes.filter(n => !n.read_at) : notes;
      return { notifications: list, unread: notes.filter(n => !n.read_at).length, total: list.length, limit: 20, page: 1, has_more: false };
    },
    "POST /api/v1/admin/notifications/(n\\d)/read": (_, m) => { notes.find(n => n.id === m[1])!.read_at = "2026-11-06T11:00:00Z"; return {}; },
  });
  await page.goto("/notifications");
  const table = page.getByRole("region", { name: "Данные: Уведомления" });
  await expect(table.getByRole("row")).toHaveCount(4);
  await expect(table.getByRole("checkbox", { name: "Выбрать строку" }).nth(2)).toBeDisabled();
  await table.getByRole("checkbox", { name: "Выбрать строки страницы" }).check();
  await page.getByRole("button", { name: "Отметить прочитанными" }).click();
  await expect(page.getByText("Отмечено прочитанными: 2")).toBeVisible();
  expect(sent.filter(r => r.method === "POST").map(r => r.path).sort()).toEqual(["/api/v1/admin/notifications/n1/read", "/api/v1/admin/notifications/n2/read"]);

  await page.getByRole("button", { name: /Непрочитанные/ }).click();
  await expect(page).toHaveURL(/unread=1/);
  await page.reload();
  await expect(page.getByRole("button", { name: /Непрочитанные/ })).toHaveAttribute("aria-pressed", "true");
});

test("логи: все фильтры переживают перезагрузку и смену раздела", async ({ page }) => {
  const sent = await mockApi(page, {
    "GET /api/v1/admin/logs": () => ({ logs: [], next_cursor: null }),
    "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
  });
  await page.goto("/logs");
  await page.getByRole("combobox", { name: "Важность" }).selectOption("ERROR");
  // Rare filters wait under "Ещё фильтры" and the panel reopens by itself once one is set.
  await expect(page.getByLabel("IP-адрес")).toHaveCount(0);
  await page.getByRole("button", { name: /Ещё фильтры/ }).click();
  await page.getByLabel("IP-адрес").fill("10.0.0.7");
  await expect(page).toHaveURL(/ip=10\.0\.0\.7/);
  await page.locator("#main-content").getByRole("link", { name: "Ошибки", exact: true }).click();
  await expect(page).toHaveURL(/\/logs\/errors\?.*severity=ERROR/);
  await page.reload();
  await expect(page.getByRole("combobox", { name: "Важность" })).toHaveValue("ERROR");
  await expect(page.getByLabel("IP-адрес")).toHaveValue("10.0.0.7");
  const last = sent.filter(r => r.path.startsWith("/api/v1/admin/logs")).at(-1)!.path;
  expect(last).toContain("ip=10.0.0.7");
  expect(last).toContain("severity=ERROR");
  expect(last).toContain("kind=errors");
});
