import { expect, test } from "@playwright/test";
import { broker, mockApi, signIn } from "./mock-api";

test.describe("несохранённые данные формы", () => {
  test.beforeEach(async ({ page }) => {
    await signIn(page, "ADMIN");
    await mockApi(page, { "GET /api/v1/admin/brokers": () => ({ brokers: [broker(1, "Альфа")] }) });
    await page.goto("/brokers");
  });

  test("пустая форма закрывается по Esc сразу", async ({ page }) => {
    await page.getByRole("button", { name: "Создать брокера" }).click();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });

  test("заполненная форма спрашивает перед закрытием и сохраняет ввод при отказе", async ({ page }) => {
    await page.getByRole("button", { name: "Создать брокера" }).click();
    const form = page.getByRole("dialog", { name: "Создать брокера" });
    await form.getByLabel("Код").fill("NEW");
    await page.keyboard.press("Escape");
    const ask = page.getByRole("dialog", { name: "Закрыть без сохранения?" });
    await expect(ask).toBeVisible();
    await ask.getByRole("button", { name: "Отмена" }).click();
    await expect(ask).toBeHidden();
    await expect(form.getByLabel("Код")).toHaveValue("NEW");

    await form.getByRole("button", { name: "Закрыть", exact: true }).click();
    await page.getByRole("dialog", { name: "Закрыть без сохранения?" }).getByRole("button", { name: "Не сохранять" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });

  test("кнопка «Отмена» в форме закрывает без вопроса", async ({ page }) => {
    await page.getByRole("button", { name: "Создать брокера" }).click();
    const form = page.getByRole("dialog", { name: "Создать брокера" });
    await form.getByLabel("Название").fill("Черновик");
    await form.getByRole("button", { name: "Отмена" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });
});
