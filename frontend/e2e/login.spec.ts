import { expect, test } from "@playwright/test";
import { mockApi, reply, sessionToken } from "./mock-api";

test.describe("вход администратора", () => {
  test("пустая форма показывает ошибки и не уходит на сервер", async ({ page }) => {
    const sent = await mockApi(page, {});
    await page.goto("/login");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByText("Укажите электронную почту")).toBeVisible();
    await expect(page.getByText("Введите пароль", { exact: true })).toBeVisible();
    await expect(page.getByLabel("Электронная почта")).toHaveAttribute("aria-invalid", "true");
    expect(sent.filter(r => r.path === "/api/v1/admin/login")).toHaveLength(0);
  });

  test("неверный пароль: сообщение сервера, страница входа остаётся", async ({ page }) => {
    await mockApi(page, { "POST /api/v1/admin/login": () => reply({ status: 401, body: { error: "Неверная почта или пароль" } }) });
    await page.goto("/login");
    await page.getByLabel("Электронная почта").fill("admin@example.test");
    await page.getByLabel("Пароль", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByRole("alert")).toHaveText("Неверная почта или пароль");
    await expect(page).toHaveURL(/\/login$/);
  });

  test("успешный вход открывает консоль и хранит сессию только во вкладке", async ({ page }) => {
    const sent = await mockApi(page, {
      "POST /api/v1/admin/login": () => ({ token: sessionToken("ADMIN"), user: { email: "admin@example.test", role: "ADMIN" } }),
      "GET /api/v1/admin/brokers": () => ({ brokers: [] }),
    });
    await page.goto("/login");
    await page.getByLabel("Электронная почта").fill("admin@example.test");
    await page.getByLabel("Пароль", { exact: true }).fill("secret");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole("navigation").first()).toBeVisible();
    expect(sent.find(r => r.path === "/api/v1/admin/login")?.body).toEqual({ email: "admin@example.test", password: "secret" });
    expect(await page.evaluate(() => [!!sessionStorage.getItem("fg_token"), localStorage.getItem("fg_token")])).toEqual([true, null]);
  });

  test("адрес без доменной зоны (admin@localhost) принимается, явная ошибка — нет", async ({ page }) => {
    const sent = await mockApi(page, { "POST /api/v1/admin/login": () => reply({ status: 401, body: { error: "invalid credentials" } }) });
    await page.goto("/login");
    await page.getByLabel("Электронная почта").fill("admin");
    await page.getByLabel("Пароль", { exact: true }).fill("x");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByText("Неверный адрес почты")).toBeVisible();
    expect(sent.filter(r => r.path === "/api/v1/admin/login")).toHaveLength(0);
    await page.getByLabel("Электронная почта").fill("admin@localhost");
    await page.getByRole("button", { name: "Войти в консоль" }).click();
    await expect(page.getByRole("alert")).toHaveText("invalid credentials");
    expect(sent.find(r => r.path === "/api/v1/admin/login")?.body).toEqual({ email: "admin@localhost", password: "x" });
  });

  test("без сессии консоль отправляет на вход", async ({ page }) => {
    await mockApi(page, {});
    await page.goto("/brokers");
    await expect(page).toHaveURL(/\/login$/);
  });
});
