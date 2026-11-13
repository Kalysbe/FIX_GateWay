// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactElement } from "react";

// The components talk to the API module only; every request is captured here.
const calls: { path: string; init?: RequestInit }[] = [];
let role = "ADMIN";
let responses: Record<string, unknown> = {};
let failWith: Error | null = null;
vi.mock("../src/api", () => ({
  getSessionUser: () => ({ role }),
  api: async (path: string, init?: RequestInit) => {
    calls.push({ path, init });
    if (init?.method && failWith) throw failWith;
    const key = Object.keys(responses).find(k => path.startsWith(k));
    return key ? responses[key] : {};
  },
}));

const { TradingLimits } = await import("../src/components/TradingLimits");
const { ReconciliationQueue } = await import("../src/components/ReconciliationQueue");
const { ExecutionInbox } = await import("../src/components/ExecutionInbox");
const { ToastProvider } = await import("../src/components/Toast");
const { ConfirmProvider } = await import("../src/components/confirm");

function mount(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, refetchInterval: false } } });
  return render(<QueryClientProvider client={client}><MemoryRouter><ToastProvider><ConfirmProvider>{ui}</ConfirmProvider></ToastProvider></MemoryRouter></QueryClientProvider>);
}
const writes = () => calls.filter(c => c.init?.method);
// Buttons of the component, not of the toast container.
const ownButtons = () => screen.queryAllByRole("button").filter(b => !b.closest("[data-sonner-toaster], section[aria-label*=\"Notifications\"]"));
const body = (i = 0) => JSON.parse(String(writes()[i].init!.body));

beforeEach(() => { calls.length = 0; role = "ADMIN"; failWith = null; responses = {}; });
afterEach(cleanup);

const policy = { broker_id: 0, paused: false, max_quantity: "10", max_open_orders: 3, revision: 2 };

test("trading limit: validation, only writable fields sent, history refreshed, form closed", async () => {
  responses = { "/api/v1/admin/trading-limits/history": { history: [], next_before_id: null }, "/api/v1/admin/trading-limits": { policies: [policy] } };
  mount(<TradingLimits />);
  fireEvent.click(await screen.findByRole("button", { name: "Изменить" }));
  fireEvent.change(screen.getByLabelText("Максимум открытых заявок"), { target: { value: "0" } });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  expect(await screen.findByText("Целое число от 1 до 1 000 000")).toBeTruthy();
  expect(screen.getByText("Не короче 3 символов")).toBeTruthy();
  expect(writes()).toHaveLength(0);

  fireEvent.change(screen.getByLabelText("Максимум открытых заявок"), { target: { value: "3" } });
  fireEvent.change(screen.getByLabelText("Причина изменения"), { target: { value: "Operator test" } });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(body()).toEqual({ broker_id: 0, paused: false, revision: 2, max_quantity: "10", max_open_orders: 3, reason: "Operator test" });
  await waitFor(() => expect(screen.queryByRole("form")).toBeNull());
  expect(screen.getByRole("status").textContent).toBe("Торговый лимит сохранён");
});

test("pausing intake asks first; cancelling sends nothing", async () => {
  responses = { "/api/v1/admin/trading-limits/history": { history: [] }, "/api/v1/admin/trading-limits": { policies: [policy] } };
  mount(<TradingLimits />);
  fireEvent.click(await screen.findByRole("button", { name: "Изменить" }));
  fireEvent.click(screen.getByLabelText("Приостановить приём новых заявок"));
  fireEvent.change(screen.getByLabelText("Причина изменения"), { target: { value: "Сбой у брокера" } });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  const dialog = await screen.findByRole("dialog", { name: "Приостановить приём заявок всех брокеров?" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Отмена" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(writes()).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Приостановить" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(body().paused).toBe(true);
});

test("a revision conflict keeps the edits and shows the API error", async () => {
  responses = { "/api/v1/admin/trading-limits/history": { history: [] }, "/api/v1/admin/trading-limits": { policies: [policy] } };
  failWith = new Error("policy changed; reload");
  mount(<TradingLimits />);
  fireEvent.click(await screen.findByRole("button", { name: "Изменить" }));
  fireEvent.change(screen.getByLabelText("Причина изменения"), { target: { value: "Operator test" } });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  expect((await screen.findByRole("status")).textContent).toBe("policy changed; reload");
  expect((screen.getByLabelText("Причина изменения") as HTMLInputElement).value).toBe("Operator test");
});

test("read-only roles see limits but cannot edit them", async () => {
  role = "READ_ONLY";
  responses = { "/api/v1/admin/trading-limits": { policies: [policy] } };
  mount(<TradingLimits />);
  expect(await screen.findByText("Все брокеры")).toBeTruthy();
  expect(ownButtons()).toHaveLength(0);
});

const reconCase = (extra: Record<string, unknown> = {}) => ({ event_id: 8, order_id: "id", client_order_id: "client", event_type: "ORDER_SEND", order_status: "ACKNOWLEDGED", last_error: "unknown", evidence_event_id: 9, resolved_at: null, actor_id: null, note: null, ...extra });

test("an unconfirmed reconciliation case has no close action", async () => {
  responses = { "/api/v1/admin/reconciliation": { cases: [reconCase({ evidence_event_id: null })] } };
  mount(<ReconciliationQueue />);
  expect(await screen.findByText(/Подтверждение отсутствует/)).toBeTruthy();
  expect(ownButtons()).toHaveLength(0);
});

test("closing a confirmed case needs a note and a confirmation, then sends the event id", async () => {
  responses = { "/api/v1/admin/reconciliation": { cases: [reconCase()] } };
  mount(<ReconciliationQueue />);
  const button = await screen.findByRole("button", { name: "Закрыть по подтверждению" }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Комментарий к случаю 8"), { target: { value: "Verified venue report" } });
  fireEvent.click(button);
  const dialog = await screen.findByRole("dialog", { name: "Закрыть случай client?" });
  expect(dialog.textContent).toContain("Verified venue report");
  fireEvent.click(within(dialog).getByRole("button", { name: "Закрыть случай" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0].path).toBe("/api/v1/admin/reconciliation/8/resolve");
  expect(body()).toEqual({ note: "Verified venue report" });
});

test("an auditor cannot close even a confirmed case", async () => {
  role = "AUDITOR";
  responses = { "/api/v1/admin/reconciliation": { cases: [reconCase()] } };
  mount(<ReconciliationQueue />);
  expect(await screen.findByText(/Есть FIX-подтверждение/)).toBeTruthy();
  expect(ownButtons()).toHaveLength(0);
  expect(screen.queryByRole("textbox")).toBeNull();
});

const blocked = {
  status: { pending: 2, bytes: 10, failed: true, healthy: false, oldest_age_seconds: 40 },
  blocked: { entry_id: "11111111-1111-1111-1111-111111111111", entry_name: "1_x.json", error: "execution correction requires venue reconciliation", failed_at: "2026-11-06T10:00:00Z", attempts: 3, read_error: false, received_at: "2026-11-06T10:00:00Z",
    report: { adapter_id: "quik-fix-1", cl_ord_id: "C1", exec_id: "E1", exec_type: "H", ord_status: "2", exec_trans_type: "0", raw_fix: "35=8|150=H", protocol_reject: false, cancel_reject: false } },
  cases: [],
};

test("quarantine needs a reason and a confirmation, and targets the blocked entry", async () => {
  role = "OPERATOR";
  responses = { "/api/v1/admin/execution-inbox": blocked };
  mount(<ExecutionInbox />);
  fireEvent.click(await screen.findByRole("button", { name: "Отложить в карантин" }));
  expect(await screen.findByText("Опишите причину и план сверки")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Причина карантина"), { target: { value: "  ARQA bust  " } });
  fireEvent.click(screen.getByRole("button", { name: "Отложить в карантин" }));
  const dialog = await screen.findByRole("dialog", { name: "Отложить отчёт в карантин?" });
  expect(dialog.textContent).toContain("C1");
  fireEvent.click(within(dialog).getByRole("button", { name: "Отложить в карантин" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0].path).toBe(`/api/v1/admin/execution-inbox/${blocked.blocked.entry_id}/quarantine`);
  expect(body()).toEqual({ reason: "ARQA bust" });
});

test("a read-only role sees the blocked report but cannot quarantine it", async () => {
  role = "AUDITOR";
  responses = { "/api/v1/admin/execution-inbox": blocked };
  mount(<ExecutionInbox />);
  expect(await screen.findByText("35=8|150=H")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Отложить в карантин" })).toBeNull();
});

test("an open quarantine case closes with its note after a confirmation", async () => {
  role = "OPERATOR";
  responses = { "/api/v1/admin/execution-inbox": { status: { pending: 0, bytes: 0, failed: false, healthy: true, oldest_age_seconds: 0 }, blocked: null,
    cases: [{ entry_id: "c1", entry_name: "n", received_at: null, adapter_id: "a", cl_ord_id: "C1", exec_id: "E1", exec_type: "H", order_id: null, apply_error: "e", attempts: 1, reason: "r", quarantined_by: "u", quarantined_at: "2026-11-06T10:00:00Z", resolved_by: null, resolved_at: null, resolution_note: null }] } };
  mount(<ExecutionInbox />);
  fireEvent.change(await screen.findByLabelText("Итог сверки c1"), { target: { value: " Сделка отменена в QUIK " } });
  fireEvent.click(screen.getByRole("button", { name: "Закрыть случай" }));
  fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Закрыть случай" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0].path).toBe("/api/v1/admin/execution-inbox/cases/c1/resolve");
  expect(body()).toEqual({ note: "Сделка отменена в QUIK" });
});
