import { expect, test } from "@playwright/test";
import { mockApi, reply, sessionToken, signIn } from "./mock-api";

test.describe("истечение сессии", () => {
  test("за 5 минут предлагает продлить паролем, страница и фильтры остаются", async ({ page }) => {
    await signIn(page, "ADMIN", 120);
    const sent = await mockApi(page, {
      "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
      "POST /api/v1/admin/login": request => request.postDataJSON().password === "secret"
        ? { token: sessionToken("ADMIN"), user: { email: "admin@example.test", role: "ADMIN" } }
        : reply({ status: 401, body: { error: "invalid credentials" } }),
    });
    await page.goto("/brokers?q=%D0%B0%D0%BB");
    const dialog = page.getByRole("dialog", { name: "Сессия скоро закончится" });
    await expect(dialog).toContainText("admin@example.test");
    await expect(dialog.getByLabel("Пароль")).toBeFocused();
    await dialog.getByLabel("Пароль").fill("wrong");
    await dialog.getByRole("button", { name: "Продлить сессию" }).click();
    await expect(dialog.getByRole("alert")).toHaveText("invalid credentials");
    await dialog.getByLabel("Пароль").fill("secret");
    await dialog.getByRole("button", { name: "Продлить сессию" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByText("Сессия продлена")).toBeVisible();
    await expect(page).toHaveURL(/\/brokers\?q=/);
    expect(sent.filter(r => r.path === "/api/v1/admin/login").at(-1)?.body).toEqual({ email: "admin@example.test", password: "secret" });
    const exp = await page.evaluate(() => JSON.parse(atob(sessionStorage.getItem("fg_token")!.split(".")[1].replace(/-/g, "+").replace(/_/g, "/"))).exp);
    expect(exp * 1000 - Date.now()).toBeGreaterThan(30 * 60_000);
  });

  test("далёкое истечение не мешает работе", async ({ page }) => {
    await signIn(page, "ADMIN", 3600);
    await mockApi(page, { "GET /api/v1/admin/brokers": () => ({ brokers: [] }) });
    await page.goto("/brokers");
    await expect(page.getByRole("heading", { name: "Брокеры" })).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });
});
