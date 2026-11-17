import { expect, test } from "@playwright/test";
import { mockApi, signIn } from "./mock-api";

const KEY_SECTIONS = ["Заявки", "Сделки", "Текущие торги", "Котировки", "Позиции"];

for (const width of [1280, 1440, 1920]) {
  test(`шапка ${width}px: ключевые разделы вверху, в меню слева их нет`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await signIn(page, "ADMIN");
    await mockApi(page, { "GET /api/v1/admin/orders": () => [] });
    await page.goto("/orders");

    const header = page.getByRole("navigation", { name: "Ключевые разделы" });
    for (const name of KEY_SECTIONS) await expect(header.getByRole("link", { name })).toBeVisible();
    await expect(header.getByRole("link", { name: "Заявки" })).toHaveAttribute("aria-current", "page");

    const sidebar = page.getByRole("navigation", { name: "Основная навигация" });
    for (const name of KEY_SECTIONS) await expect(sidebar.getByRole("link", { name, exact: true })).toBeHidden();
    await expect(sidebar.getByRole("link", { name: "Инструменты" })).toBeVisible();
    await expect(sidebar.getByRole("link", { name: "Уведомления" })).toHaveCount(0);

    // Everything in the header fits on one line, without a side scroll.
    expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(0);
    const bar = (await page.locator(".app-header").boundingBox())!;
    expect(Math.round(bar.height)).toBe(56);

    // The profile menu holds the sign-out.
    await page.getByRole("button", { name: /Профиль/ }).click();
    await expect(page.getByRole("menuitem", { name: "Выйти" })).toBeVisible();
    await page.screenshot({ path: info.outputPath(`header-${width}.png`) });
  });
}

test("шапка уже 1280px: разделы остаются в меню слева", async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 800 });
  await signIn(page, "ADMIN");
  await mockApi(page, { "GET /api/v1/admin/orders": () => [] });
  await page.goto("/orders");
  await expect(page.getByRole("navigation", { name: "Ключевые разделы" })).toBeHidden();
  await expect(page.getByRole("navigation", { name: "Основная навигация" }).getByRole("link", { name: "Сделки" })).toBeVisible();
});
