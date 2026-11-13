import { expect, test } from "@playwright/test";
import { broker, mockApi, reply, signIn } from "./mock-api";

const order = (n: number, extra: Record<string, unknown> = {}) => ({ OrderNum: 7000 + n, OrderDateTime: `2026-11-06 10:0${n}:00`, TradeDate: "2026-11-06", ClassCode: "LIST_GBA", SecCode: `GBA0${n}`, Operation: n % 2 ? "Купля" : "Продажа", State: "Активна", Price: 99 + n, Qty: 10, Balance: 10, FirmId: "B1", ...extra });

test.beforeEach(async ({ page }) => signIn(page, "ADMIN"));

test("блокировка брокера спрашивает подтверждение и сообщает результат", async ({ page }) => {
  const brokers = [broker(1, "Альфа Капитал")];
  const sent = await mockApi(page, {
    "GET /api/v1/admin/brokers": () => ({ brokers }),
    "PATCH /api/v1/admin/brokers/1": request => { brokers[0] = { ...brokers[0], ...request.postDataJSON() }; return { broker: brokers[0] }; },
  });
  await page.goto("/brokers");
  await page.getByRole("button", { name: "Заблокировать" }).click();
  const dialog = page.getByRole("dialog", { name: "Заблокировать «Альфа Капитал»?" });
  await expect(dialog).toContainText("Новые запросы с токенами брокера будут отклоняться");
  await dialog.getByRole("button", { name: "Отмена" }).click();
  expect(sent.some(r => r.method === "PATCH")).toBe(false);

  await page.getByRole("button", { name: "Заблокировать" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Заблокировать" }).click();
  await expect(page.getByText("Брокер «Альфа Капитал» заблокирован")).toBeVisible();
  expect(sent.find(r => r.method === "PATCH")?.body).toEqual({ status: "BLOCKED" });
  await expect(page.getByRole("button", { name: "Активировать" })).toBeVisible();
});

test("ошибка сервера остаётся в диалоге подтверждения", async ({ page }) => {
  await mockApi(page, {
    "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа Капитал")] }),
    "PATCH /api/v1/admin/brokers/1": () => reply({ status: 403, body: { error: "Недостаточно прав" } }),
  });
  await page.goto("/brokers");
  await page.getByRole("button", { name: "Заблокировать" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Заблокировать" }).click();
  await expect(dialog.getByRole("alert")).toHaveText("Недостаточно прав");
  await expect(dialog).toBeVisible();
});

test("строка заявки открывается в боковой панели, ↓ ведёт к следующей", async ({ page }) => {
  await mockApi(page, {
    "GET /api/v1/admin/orders": () => [order(1), order(2), order(3)],
    "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
  });
  await page.goto("/orders");
  await page.getByRole("region", { name: "Данные: Заявки" }).getByRole("row").nth(2).getByRole("cell").nth(2).click();
  const drawer = page.getByRole("dialog", { name: "Заявка 7002" });
  await expect(drawer).toContainText("GBA02");
  await expect(drawer.getByRole("link", { name: "Карточка заявки" })).toHaveAttribute("href", "/orders/quik-20261106-LIST_GBA-7002");
  await page.keyboard.press("ArrowDown");
  await expect(page.getByRole("dialog", { name: "Заявка 7003" })).toBeVisible();
  await page.getByRole("dialog").getByLabel("Поиск по полям строки").fill("balance");
  await expect(page.getByRole("dialog").locator("dt")).toHaveText(["Balance"]);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeHidden();
});

test("фильтр, поиск и сортировка переживают перезагрузку страницы", async ({ page }) => {
  await mockApi(page, {
    "GET /api/v1/admin/orders": () => [order(1), order(2), order(3)],
    "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа Капитал")] }),
  });
  await page.goto("/orders");
  await page.getByLabel("Брокер").selectOption("1");
  await page.getByRole("searchbox", { name: "Поиск: Заявки" }).fill("GBA0");
  await page.getByRole("button", { name: "Цена" }).click();
  await page.getByRole("button", { name: "Цена" }).click();
  await expect(page).toHaveURL(/broker=1/);
  await page.reload();
  await expect(page.getByLabel("Брокер")).toHaveValue("1");
  await expect(page.getByRole("searchbox", { name: "Поиск: Заявки" })).toHaveValue("GBA0");
  await expect(page.getByRole("columnheader", { name: "Цена" })).toHaveAttribute("aria-sort", "descending");
});

test("живая таблица подсвечивает изменённую заявку и направление цены", async ({ page }) => {
  let calls = 0;
  await mockApi(page, {
    "GET /api/v1/admin/orders": () => (++calls === 1 ? [order(1), order(2)] : [order(1, { Balance: 4 }), order(2), order(3)]),
    "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
  });
  await page.goto("/orders");
  await expect(page.getByRole("region", { name: "Данные: Заявки" }).getByRole("row")).toHaveCount(3);
  // The page polls every 3 s while the event stream is down.
  await expect(page.getByRole("status").filter({ hasText: "+1 новая строка" })).toBeVisible({ timeout: 8000 });
  await expect(page.locator("tr.row-flash-changed td.cell-down")).toHaveCount(1);
  await expect(page.locator("tr.row-flash-new")).toHaveCount(1);
});

test("массовый отзыв токенов: выбор, подтверждение, запросы", async ({ page }) => {
  const token = (id: string, name: string, status = "ACTIVE") => ({ id, name, status, token_prefix: "fgw_" + id, permissions: ["orders:read"], created_at: "2026-11-01T09:00:00Z" });
  const tokens = [token("t1", "Prod"), token("t2", "Backup"), token("t3", "Old", "REVOKED")];
  const sent = await mockApi(page, {
    "GET /api/v1/admin/brokers/1": () => ({ broker: broker(1, "Альфа Капитал") }),
    "GET /api/v1/admin/brokers/1/tokens": () => ({ tokens }),
    "GET /api/v1/admin/brokers/1/security": () => ({ security: {} }),
    "GET /api/v1/admin/brokers/1/ip-whitelist": () => ({ entries: [] }),
    "GET /api/v1/admin/brokers/1/accounts": () => [],
    "GET /api/v1/admin/brokers/1/stats": () => ({ stats: { series: [] } }),
    "GET /api/v1/admin/brokers/1/logs": () => ({ logs: [] }),
    "POST /api/v1/admin/tokens/(t1|t2)/revoke": (_, m) => { tokens.find(t => t.id === m[1])!.status = "REVOKED"; return reply({ status: 204 }); },
  });
  await page.goto("/brokers/1?tab=tokens");
  const table = page.getByRole("region", { name: "Данные: Токены брокера" });
  await expect(table.getByRole("checkbox", { name: "Выбрать строку" }).nth(2)).toBeDisabled();
  await table.getByRole("checkbox", { name: "Выбрать строки страницы" }).check();
  await page.getByRole("button", { name: "Отозвать выбранные" }).click();
  const dialog = page.getByRole("dialog", { name: "Отозвать токены: 2?" });
  await expect(dialog).toContainText("Prod, Backup");
  await dialog.getByRole("button", { name: "Отозвать все" }).click();
  await expect(page.getByText("Отозвано токенов: 2")).toBeVisible();
  expect(sent.filter(r => r.method === "POST" && r.path.endsWith("/revoke")).map(r => r.path).sort()).toEqual(["/api/v1/admin/tokens/t1/revoke", "/api/v1/admin/tokens/t2/revoke"]);
});

test("плитки дашборда ведут в нужные разделы", async ({ page }) => {
  await mockApi(page, {
    "GET /api/v1/admin/dashboard": () => ({ system: { api: "ONLINE", database: "ONLINE", queue: "ONLINE" }, adapters: [], stats: { total: 10, filled: 4, rejected: 1, active: 5 }, brokers: 1, adapters_available: 2 }),
    "GET /api/v1/admin/orders": () => [order(1)],
    "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
  });
  await page.goto("/");
  await page.getByRole("link", { name: /Активных/ }).click();
  await expect(page).toHaveURL(/\/orders$/);
  await expect(page.getByRole("heading", { name: "Заявки" })).toBeVisible();
});
