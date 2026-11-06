import { expect, test } from "@playwright/test";
import { broker, mockApi, reply, signIn } from "./mock-api";

test.describe("брокеры", () => {
  test.beforeEach(async ({ page }) => signIn(page, "ADMIN"));

  test("создание брокера: проверка полей, запрос и обновление списка", async ({ page }) => {
    const brokers = [broker(1, "Альфа Капитал"), broker(2, "Бета Инвест", { status: "BLOCKED" })];
    const sent = await mockApi(page, {
      "GET /api/v1/admin/brokers": () => ({ brokers }),
      "POST /api/v1/admin/brokers": request => {
        const body = request.postDataJSON();
        brokers.push(broker(3, body.name, { code: body.code }));
        return reply({ status: 201, body: { broker: brokers.at(-1) } });
      },
    });
    await page.goto("/brokers");
    const table = page.getByRole("region", { name: "Данные: Брокеры" });
    await expect(table.getByRole("row")).toHaveCount(3);

    await page.getByRole("button", { name: "Создать брокера" }).click();
    const dialog = page.getByRole("dialog", { name: "Создать брокера" });
    await expect(dialog.getByLabel("Код")).toBeFocused();
    await dialog.getByRole("button", { name: "Создать" }).click();
    await expect(dialog.getByText("Укажите код брокера")).toBeVisible();
    await expect(dialog.getByText("Укажите название")).toBeVisible();

    await dialog.getByLabel("Код").fill("GAMMA");
    await dialog.getByLabel("Название").fill("Гамма Брокер");
    await dialog.getByRole("button", { name: "Создать" }).click();
    await expect(dialog).toBeHidden();
    await expect(table.getByRole("link", { name: "Гамма Брокер" })).toBeVisible();
    expect(sent.find(r => r.method === "POST" && r.path === "/api/v1/admin/brokers")?.body).toEqual({ code: "GAMMA", name: "Гамма Брокер", description: "" });
    // Focus returns to the button that opened the dialog.
    await expect(page.getByRole("button", { name: "Создать брокера" })).toBeFocused();
  });

  test("ошибка сервера остаётся в форме, окно не закрывается", async ({ page }) => {
    await mockApi(page, {
      "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
      "POST /api/v1/admin/brokers": () => reply({ status: 409, body: { error: "Брокер с таким кодом уже есть" } }),
    });
    await page.goto("/brokers");
    await page.getByRole("button", { name: "Создать брокера" }).click();
    const dialog = page.getByRole("dialog", { name: "Создать брокера" });
    await dialog.getByLabel("Код").fill("B1");
    await dialog.getByLabel("Название").fill("Дубль");
    await dialog.getByRole("button", { name: "Создать" }).click();
    await expect(dialog.getByRole("alert")).toHaveText("Брокер с таким кодом уже есть");
    await dialog.getByRole("button", { name: "Отмена" }).click();
    await expect(dialog).toBeHidden();
  });

  test("поиск и сортировка таблицы", async ({ page }) => {
    await mockApi(page, { "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа"), broker(2, "Бета"), broker(3, "Вега")] }) });
    await page.goto("/brokers");
    const table = page.getByRole("region", { name: "Данные: Брокеры" });
    await page.getByRole("searchbox", { name: "Поиск: Брокеры" }).fill("бет");
    await expect(table.getByRole("row")).toHaveCount(2);
    await page.getByRole("searchbox", { name: "Поиск: Брокеры" }).fill("");
    await table.getByRole("button", { name: "Брокер" }).click();
    await table.getByRole("button", { name: "Брокер" }).click();
    await expect(table.getByRole("columnheader", { name: "Брокер" })).toHaveAttribute("aria-sort", "descending");
    await expect(table.getByRole("row").nth(1)).toContainText("Вега");
  });
});

test.describe("подтверждение опасных действий", () => {
  test("отзыв токена спрашивает подтверждение и отправляет запрос только после него", async ({ page }) => {
    await signIn(page, "ADMIN");
    const token = { id: "tok-1", name: "Production API", status: "ACTIVE", token_prefix: "fgw_abcd", permissions: ["orders:read"], created_at: "2026-11-01T09:00:00Z", quik_firm: "B1" };
    const sent = await mockApi(page, {
      "GET /api/v1/admin/brokers/1": () => ({ broker: broker(1, "Альфа Капитал") }),
      "GET /api/v1/admin/brokers/1/tokens": () => ({ tokens: [token] }),
      "GET /api/v1/admin/brokers/1/security": () => ({ security: { active_tokens: 1 } }),
      "GET /api/v1/admin/brokers/1/ip-whitelist": () => ({ entries: [] }),
      "GET /api/v1/admin/brokers/1/accounts": () => [],
      "GET /api/v1/admin/brokers/1/stats": () => ({ stats: { series: [] } }),
      "GET /api/v1/admin/brokers/1/logs": () => ({ logs: [] }),
      "POST /api/v1/admin/tokens/tok-1/revoke": () => reply({ status: 204 }),
    });
    await page.goto("/brokers/1?tab=tokens");
    await page.getByRole("button", { name: /Действия/ }).first().click();
    await page.getByRole("menuitem", { name: "Отозвать токен" }).click();
    const dialog = page.getByRole("dialog", { name: "Отозвать токен?" });
    await expect(dialog).toContainText("Production API");
    await dialog.getByRole("button", { name: "Отмена" }).click();
    await expect(dialog).toBeHidden();
    expect(sent.some(r => r.path.endsWith("/revoke"))).toBe(false);

    await page.getByRole("button", { name: /Действия/ }).first().click();
    await page.getByRole("menuitem", { name: "Отозвать токен" }).click();
    await page.getByRole("dialog", { name: "Отозвать токен?" }).getByRole("button", { name: /Отозвать/ }).click();
    await expect(page.getByRole("dialog", { name: "Отозвать токен?" })).toBeHidden();
    expect(sent.filter(r => r.method === "POST" && r.path === "/api/v1/admin/tokens/tok-1/revoke")).toHaveLength(1);
  });
});
