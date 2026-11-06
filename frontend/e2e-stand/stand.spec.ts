import { expect, test, type Page } from "@playwright/test";

// Local stand credentials from deploy/docker-compose.local.yml (development only).
const EMAIL = process.env.STAND_EMAIL ?? "admin@localhost";
const PASSWORD = process.env.STAND_PASSWORD ?? "admin123";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Электронная почта").fill(EMAIL);
  await page.getByLabel("Пароль", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Войти в консоль" }).click();
  await expect(page).toHaveURL(/\/$/);
}

test("вход, живой поток событий и дашборд настоящего шлюза", async ({ page }) => {
  await login(page);
  // The admin SSE stream goes through the gateway's own TLS gRPC bridge.
  await expect(page.getByText("События live")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole("link", { name: /Активных/ })).toBeVisible();
  await page.getByRole("link", { name: /Брокеров онлайн/ }).click();
  await expect(page).toHaveURL(/\/brokers$/);
});

test("создание, блокировка с подтверждением и активация брокера", async ({ page }) => {
  const code = `E2E${Date.now().toString(36).toUpperCase()}`;
  await login(page);
  await page.goto("/brokers");
  await page.getByRole("button", { name: "Создать брокера" }).click();
  const form = page.getByRole("dialog", { name: "Создать брокера" });
  await form.getByLabel("Код").fill(code);
  await form.getByLabel("Название").fill(`Проверка ${code}`);
  await form.getByRole("button", { name: "Создать" }).click();
  await expect(page.getByText(`Брокер «Проверка ${code}» создан`)).toBeVisible();

  await page.getByRole("searchbox", { name: "Поиск: Брокеры" }).fill(code);
  const row = page.getByRole("region", { name: "Данные: Брокеры" }).getByRole("row").filter({ hasText: code });
  await row.getByRole("button", { name: "Заблокировать" }).click();
  await page.getByRole("dialog", { name: `Заблокировать «Проверка ${code}»?` }).getByRole("button", { name: "Заблокировать" }).click();
  await expect(page.getByText(`Брокер «Проверка ${code}» заблокирован`)).toBeVisible();
  await expect(row.getByText("Заблокирован", { exact: false })).toBeVisible();
  await row.getByRole("button", { name: "Активировать" }).click();
  await expect(page.getByText(`Брокер «Проверка ${code}» активирован`)).toBeVisible();
  // The search survives a reload.
  await page.reload();
  await expect(page.getByRole("searchbox", { name: "Поиск: Брокеры" })).toHaveValue(code);
});

test("страницы данных открываются без ошибок интерфейса", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", e => errors.push(e.message));
  await login(page);
  for (const path of ["/orders", "/trades", "/quotes", "/market-data", "/positions", "/notifications", "/logs", "/monitoring", "/adapters", "/rate-limits", "/execution-inbox"]) {
    await page.goto(path);
    await expect(page.locator("#main-content h1").first()).toBeVisible();
    await expect(page.getByText("Ошибка интерфейса")).toHaveCount(0);
  }
  expect(errors).toEqual([]);
});
