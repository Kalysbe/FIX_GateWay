// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render as rtlRender, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactElement } from "react";
import { DataTable, type ServerPage, type TableColumn } from "../src/components/DataTable";

afterEach(cleanup);
const render = (ui: ReactElement) => { const r = rtlRender(ui, { wrapper: ({ children }) => <MemoryRouter>{children}</MemoryRouter> }); return { ...r, rerender: (next: ReactElement) => r.rerender(next) }; };

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

test("a live table marks new rows and the direction of a changed trend value, not the first load", () => {
  const trendCols: TableColumn<Row>[] = [{ id: "symbol", header: "Инструмент", value: r => r.symbol }, { id: "price", header: "Цена", value: r => r.price, numeric: true, trend: true }];
  const table = (data: Row[], resetKey = "") => <DataTable title="Live" live rows={data} columns={trendCols} rowKey={r => r.id} resetKey={resetKey} />;
  const { rerender } = render(table(rows(2)));
  expect(document.querySelector(".row-flash-new, .row-flash-changed")).toBeNull();
  rerender(table([{ id: 0, symbol: "AAA", price: 3 }, { id: 1, symbol: "AAA", price: 1 }, { id: 2, symbol: "BBB", price: 2 }]));
  const [first, second, third] = bodyRows();
  expect(first.className).toContain("row-flash-changed");
  expect(within(first).getAllByRole("cell")[1].className).toContain("cell-up");
  expect(within(second).getAllByRole("cell")[1].className).toContain("cell-down");
  expect(third.className).toContain("row-flash-new");
  expect(screen.getByRole("status").textContent).toBe("+1 новая строка");
  // Another filter or server page is a new baseline: nothing flashes.
  rerender(table(rows(4), "other"));
  expect(document.querySelector(".row-flash-new, .row-flash-changed")).toBeNull();
});

test("selected rows get bulk actions and the selection clears after they run", async () => {
  const run = vi.fn(async () => {});
  render(<DataTable title="Sel" rows={rows(3)} columns={columns} rowKey={r => r.id} selection={{ canSelect: r => r.id !== 2, actions: [{ label: "Отметить", run }] }} />);
  const boxes = screen.getAllByRole("checkbox", { name: "Выбрать строку" }) as HTMLInputElement[];
  expect(boxes[2].disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: "Выбрать строки страницы" }));
  expect(screen.getByRole("region", { name: "Действия с выбранными строками" }).textContent).toContain("Выбрано: 2");
  fireEvent.click(screen.getByRole("button", { name: "Отметить" }));
  await vi.waitFor(() => expect(run).toHaveBeenCalledWith([expect.objectContaining({ id: 0 }), expect.objectContaining({ id: 1 })]));
  await vi.waitFor(() => expect(screen.queryByRole("region", { name: "Действия с выбранными строками" })).toBeNull());
});

test("search, sort and page come back from the URL", () => {
  rtlRender(<MemoryRouter initialEntries={["/x?q=AAA&sort=price:desc&page=2&rows=10"]}><DataTable title="Url" urlState rows={rows(30, id => ({ price: id }))} columns={columns} rowKey={r => r.id} /></MemoryRouter>);
  expect((search() as HTMLInputElement).value).toBe("AAA");
  expect(screen.getByRole("columnheader", { name: "Цена" }).getAttribute("aria-sort")).toBe("descending");
  expect(pager().textContent).toContain("11–20");
  expect(within(bodyRows()[0]).getAllByRole("cell")[1].textContent).toBe("19");
});

test("a row opens by click or Enter, but not from a link or button inside it", () => {
  const open = vi.fn();
  const linked: TableColumn<Row>[] = [...columns.slice(0, 2), { id: "go", header: "", value: () => "", cell: () => <button type="button">Действие</button> }];
  render(<DataTable title="Click" rows={rows(2)} columns={linked} rowKey={r => r.id} onRowClick={open} />);
  fireEvent.click(screen.getAllByRole("button", { name: "Действие" })[0]);
  expect(open).not.toHaveBeenCalled();
  fireEvent.click(bodyRows()[1].querySelector("td")!);
  expect(open).toHaveBeenLastCalledWith(expect.objectContaining({ id: 1 }), expect.any(Array));
  fireEvent.keyDown(bodyRows()[0], { key: "Enter" });
  expect(open).toHaveBeenLastCalledWith(expect.objectContaining({ id: 0 }), expect.any(Array));
});