import { expect, test } from "@playwright/test";
import { mockApi, signIn, type Role } from "./mock-api";

const limits = {
  policies: [{ scope: "broker", subject: "1", operation: "SubmitOrder", rate: 50, burst: 50, concurrent: 8, enabled: true }],
  usage: [],
  operations: ["*", "SubmitOrder", "CancelOrder", "Trading", "Queries", "System", "Connect", "Subscribe", "Ping"],
};
const routes = {
  "GET /api/v1/admin/rate-limits": () => limits,
  "GET /api/v1/admin/trading-limits": () => ({ policies: [] }),
  "GET /api/v1/admin/trading-limits/history": () => ({ history: [], next_before_id: null }),
  "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
};

test.describe("роли и права на лимиты", () => {
  for (const role of ["VIEWER", "OPERATOR"] as Role[]) {
    test(`${role} видит лимиты, но не может их менять`, async ({ page }) => {
      await signIn(page, role);
      await mockApi(page, routes);
      await page.goto("/rate-limits");
      await expect(page.getByRole("region", { name: "Данные: Лимиты API" }).getByRole("row")).toHaveCount(2);
      await expect(page.getByRole("button", { name: "Изменить" })).toHaveCount(0);
      await expect(page.getByLabel("Причина изменения")).toHaveCount(0);
    });
  }

  test("ADMIN меняет лимит: проверка полей и отправленное правило", async ({ page }) => {
    await signIn(page, "ADMIN");
    const sent = await mockApi(page, { ...routes, "PUT /api/v1/admin/rate-limits": () => ({ ok: true }) });
    await page.goto("/rate-limits");
    await page.getByRole("button", { name: "Изменить" }).click();
    await expect(page.getByLabel("Broker ID")).toHaveValue("1");
    await page.getByLabel("rate").fill("0");
    await page.getByRole("button", { name: "Сохранить" }).click();
    await expect(page.getByText("Не меньше 1")).toBeVisible();
    await expect(page.getByText("Не короче 3 символов")).toBeVisible();
    expect(sent.some(r => r.method === "PUT")).toBe(false);

    await page.getByLabel("rate").fill("120");
    await page.getByLabel("Причина изменения").fill("Рост нагрузки");
    await page.getByRole("button", { name: "Сохранить" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Лимит сохранён" })).toBeVisible();
    expect(sent.find(r => r.method === "PUT")?.body).toEqual({ scope: "broker", subject: "1", operation: "SubmitOrder", rate: 120, burst: 50, concurrent: 8, enabled: true, reason: "Рост нагрузки" });
  });

  test("смена области подставляет субъект и операцию", async ({ page }) => {
    await signIn(page, "SUPER_ADMIN");
    await mockApi(page, routes);
    await page.goto("/rate-limits");
    await page.getByLabel("Scope").selectOption("global");
    await expect(page.getByLabel("Broker ID")).toHaveValue("*");
    await expect(page.getByLabel("Broker ID")).toHaveAttribute("readonly", "");
    await expect(page.getByLabel("Operation")).toHaveValue("Trading");
  });
});
