import { expect, test } from "@playwright/test";
import { mockApi, reply, sessionToken } from "./mock-api";

test.describe("вход администратора", () => {
  test("пустая форма показывает ошибки и не уходит на сервер", async ({ page }) => {
    const sent = await mockApi(page, {});
    await page.goto("/login");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByText("Укажите логин")).toBeVisible();
    await expect(page.getByText("Введите пароль", { exact: true })).toBeVisible();
    await expect(page.getByLabel("Логин")).toHaveAttribute("aria-invalid", "true");
    expect(sent.filter(r => r.path === "/api/v1/admin/login")).toHaveLength(0);
  });

  test("неверный пароль: сообщение сервера, страница входа остаётся", async ({ page }) => {
    await mockApi(page, { "POST /api/v1/admin/login": () => reply({ status: 401, body: { error: "Неверный логин или пароль" } }) });
    await page.goto("/login");
    await page.getByLabel("Логин").fill("admin");
    await page.getByLabel("Пароль", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByRole("alert")).toHaveText("Неверный логин или пароль");
    await expect(page).toHaveURL(/\/login$/);
  });

  test("успешный вход открывает консоль и хранит сессию только во вкладке", async ({ page }) => {
    const sent = await mockApi(page, {
      "POST /api/v1/admin/login": () => ({ token: sessionToken("ADMIN"), user: { username: "admin", role: "ADMIN" } }),
      "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
    });
    await page.goto("/login");
    await page.getByLabel("Логин").fill("  admin ");
    await page.getByLabel("Пароль", { exact: true }).fill("secret");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole("navigation").first()).toBeVisible();
    // The login is sent trimmed; the password exactly as typed.
    expect(sent.find(r => r.path === "/api/v1/admin/login")?.body).toEqual({ username: "admin", password: "secret" });
    expect(await page.evaluate(() => [!!sessionStorage.getItem("fg_token"), localStorage.getItem("fg_token")])).toEqual([true, null]);
  });

  test("без сессии консоль отправляет на вход", async ({ page }) => {
    await mockApi(page, {});
    await page.goto("/brokers");
    await expect(page).toHaveURL(/\/login$/);
  });
});
