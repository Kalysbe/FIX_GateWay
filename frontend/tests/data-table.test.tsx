// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { DataTable, type ServerPage, type TableColumn } from "../src/components/DataTable";

afterEach(cleanup);

type Row = { id: number; symbol: string; price: number };
const columns: TableColumn<Row>[] = [
  { id: "symbol", header: "Инструмент", value: r => r.symbol },
  { id: "price", header: "Цена", value: r => r.price, numeric: true },
  { id: "note", header: "Примечание", value: () => "скрыто", defaultHidden: true },
];
const rows = (n: number, row: (id: number) => Partial<Row> = () => ({})) =>
  Array.from({ length: n }, (_, id) => ({ id, symbol: "AAA", price: 2, ...row(id) }));
const view = (data: Row[], extra: { serverPage?: ServerPage } = {}) =>
  render(<DataTable title="Orders" rows={data} columns={columns} rowKey={r => r.id} summary={list => [{ label: "Сумма", value: list.reduce((n, r) => n + r.price, 0) }]} {...extra} />);
const bodyRows = () => within(screen.getByRole("table")).getAllByRole("row").slice(1);
const search = () => screen.getByRole("searchbox");
const pager = () => screen.getByText(/из \d/).parentElement!;

test("table totals use every filtered row, independent of the current page", () => {
  view(rows(25, id => ({ symbol: id < 23 ? "AAA" : "BBB" })));
  expect(screen.getByText("Сумма").textContent).toBe("Сумма50");
  expect(bodyRows()).toHaveLength(20);
  fireEvent.change(search(), { target: { value: "AAA" } });
  expect(screen.getByText("Сумма").textContent).toBe("Сумма46");
  expect(pager().textContent).toContain("из 23");
});

test("sorting is accessible and resets pagination; a search with no match says so", () => {
  view(rows(25, id => ({ price: id })));
  fireEvent.click(screen.getByLabelText("Следующая страница"));
  expect(pager().textContent).toContain("21–25");
  fireEvent.click(screen.getByRole("button", { name: "Цена" }));
  expect(screen.getByRole("columnheader", { name: "Цена" }).getAttribute("aria-sort")).toBe("ascending");
  expect(pager().textContent).toContain("1–20");
  fireEvent.click(screen.getByRole("button", { name: "Цена" }));
  expect(screen.getByRole("columnheader", { name: "Цена" }).getAttribute("aria-sort")).toBe("descending");
  expect(within(bodyRows()[0]).getAllByRole("cell")[1].textContent).toBe("24");
  fireEvent.change(search(), { target: { value: "not-found" } });
  expect(screen.getByText("Ничего не найдено")).toBeTruthy();
});

test("a shrinking live dataset stays on a valid page", () => {
  const { rerender } = view(rows(25));
  fireEvent.click(screen.getByLabelText("Последняя страница"));
  expect(pager().textContent).toContain("21–25");
  rerender(<DataTable title="Orders" rows={rows(5)} columns={columns} rowKey={r => r.id} />);
  expect(pager().textContent).toContain("1–5");
  expect(bodyRows()).toHaveLength(5);
});

test("hidden columns start in the menu and can be shown", () => {
  view(rows(2));
  expect(screen.queryByRole("columnheader", { name: "Примечание" })).toBeNull();
  fireEvent.click(screen.getByRole("checkbox", { name: "Примечание" }));
  expect(screen.getByRole("columnheader", { name: "Примечание" })).toBeTruthy();
});

test("server pages move from the table footer", () => {
  const onNext = vi.fn(), onPrevious = vi.fn();
  view(rows(3), { serverPage: { start: 201, end: 203, size: 200, hasPrevious: true, hasNext: false, onNext, onPrevious } });
  const group = screen.getByRole("group", { name: "Страницы источника" });
  expect(group.textContent).toContain("Строки БД 201–203");
  fireEvent.click(within(group).getByRole("button", { name: /Предыдущие 200/ }));
  expect(onPrevious).toHaveBeenCalledOnce();
  expect((within(group).getByRole("button", { name: /Следующие 200/ }) as HTMLButtonElement).disabled).toBe(true);
});
