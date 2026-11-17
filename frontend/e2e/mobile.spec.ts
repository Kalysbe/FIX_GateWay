import { expect, test, type Page } from "@playwright/test";
import { broker, mockApi, signIn } from "./mock-api";

const order = (n: number) => ({ OrderNum: 7000 + n, OrderDateTime: `2026-11-06 10:0${n}:00`, TradeDate: "2026-11-06", ClassCode: "LIST_GBA", SecCode: `GBA0${n}`, Operation: n % 2 ? "Купля" : "Продажа", State: "Активна", Price: 99 + n, Qty: 10, Balance: 10, FirmId: "B1" });

test.use({ viewport: { width: 375, height: 760 }, isMobile: true, hasTouch: true });

const noSideScroll = async (page: Page) => expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(0);

test("телефон: таблица, выбор, панель строки и диалог помещаются в экран", async ({ page }) => {
  await signIn(page, "ADMIN");
  await mockApi(page, {
    "GET /api/v1/admin/orders": () => [1, 2, 3, 4].map(order),
    "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа Капитал")] }),
  });
  await page.goto("/orders");
  const table = page.getByRole("region", { name: "Данные: Заявки" });
  await table.getByRole("checkbox", { name: "Выбрать строку" }).first().check();
  await expect(page.getByRole("region", { name: "Действия с выбранными строками" })).toBeVisible();
  await noSideScroll(page);
  // The page-size control shows its value.
  const size = page.getByRole("combobox", { name: "Строк на странице" });
  expect(await size.evaluate(el => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThan(10);
  // Server paging buttons sit side by side, inside the screen.
  for (const name of [/Предыдущие 200/, /Следующие 200/]) {
    const box = (await page.getByRole("button", { name }).boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(375);
  }

  await table.getByRole("row").nth(2).getByRole("cell").nth(2).click();
  const drawer = page.getByRole("dialog", { name: "Заявка 7002" });
  const box = (await drawer.boundingBox())!;
  expect(Math.round(box.width)).toBe(375);
  await noSideScroll(page);
  await page.keyboard.press("Escape");

  await page.goto("/brokers");
  await page.getByRole("button", { name: "Заблокировать" }).click();
  const dialog = (await page.getByRole("dialog").boundingBox())!;
  expect(dialog.x).toBeGreaterThanOrEqual(0);
  expect(dialog.x + dialog.width).toBeLessThanOrEqual(375);
});
