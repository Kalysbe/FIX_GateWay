import { RowActions } from "../components/RowActions";
import { DataTable } from "../components/DataTable";
import { dateValue } from "../lib/table";
import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { formatDate, formatDateTime, formatTime } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { Icons } from "../components/icons";
import { StatusBadge } from "../components/StatusBadge";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { Modal } from "../components/Modal";
import { ErrorBanner, PageSpinner } from "../components/feedback";
import { ALL_PERMS, READ_ONLY_PERMS, type APIToken, type AuditRow, type Broker, type BrokerAccount, type IPWhitelistEntry, type ObsLog } from "../types/admin";
import {
  ActivityList,
  BrokerActivity,
  DetailRow,
  Metric,
  PERM_ICON,
  RequestChart,
  SummaryChip,
  pctDelta,
  type BrokerAPIStats,
} from "./broker/widgets";

const READ_ONLY = READ_ONLY_PERMS;

const TABS = [
  { id: "overview", label: "Обзор" },
  { id: "tokens", label: "Токены" },
  { id: "accounts", label: "Счета" },
  { id: "ip", label: "IP Whitelist" },
  { id: "rights", label: "Права" },
  { id: "stats", label: "Статистика" },
  { id: "activity", label: "Активность" },
  { id: "logs", label: "Логи" },
  { id: "history", label: "История" },
] as const;

type TabId = (typeof TABS)[number]["id"];

type BrokerStatsDTO = {
  days: number;
  today: { orders: number; filled: number; rejected: number; active: number };
  period: { orders: number; filled: number; rejected: number; active: number };
  previous: { orders: number; filled: number; rejected: number; active: number };
  series: Array<{ date: string; orders: number }>;
};

export default function BrokerDetailPage() {
  const { id } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as TabId) || "tokens";
  const qc = useQueryClient();
  const { connected } = useAdminStream();

  const broker = useQuery({
    queryKey: ["broker", id],
    queryFn: ({ signal }) => api<{ broker: Broker }>(`/api/v1/admin/brokers/${id}`, { signal }),
  });
  const accounts = useQuery({
    queryKey: ["accounts", id],
    queryFn: ({ signal }) => api<{ accounts: BrokerAccount[] }>(`/api/v1/admin/brokers/${id}/accounts`, { signal }),
  });
  const security = useQuery({
    queryKey: ["security", id],
    queryFn: ({ signal }) =>
      api<{ security: { active_tokens?: number; revoked_tokens?: number; failed_auth_attempts_24h?: number } }>(
        `/api/v1/admin/brokers/${id}/security`,
        { signal },
      ),
  });
  const tokens = useQuery({
    queryKey: ["tokens", id],
    queryFn: ({ signal }) => api<{ tokens: APIToken[] }>(`/api/v1/admin/brokers/${id}/tokens`, { signal }),
    enabled: tab === "tokens" || tab === "overview" || tab === "rights",
  });
  const whitelist = useQuery({
    queryKey: ["ipwl", id],
    queryFn: ({ signal }) =>
      api<{ entries: IPWhitelistEntry[]; ip_whitelist_enabled?: boolean }>(`/api/v1/admin/brokers/${id}/ip-whitelist`, { signal }),
    enabled: tab === "ip" || tab === "tokens" || tab === "overview",
  });
  const audit = useQuery({
    queryKey: ["audit"],
    queryFn: ({ signal }) => api<{ audit: AuditRow[] }>("/api/v1/admin/audit", { signal }),
    enabled: tab === "history" || tab === "overview",
  });
  const [statsDays, setStatsDays] = useState(7);
  const brokerStats = useQuery({
    queryKey: ["broker-stats", id, statsDays],
    queryFn: ({ signal }) => api<{ stats: BrokerStatsDTO }>(`/api/v1/admin/brokers/${id}/stats?days=${statsDays}`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 15000),
    enabled: tab === "tokens" || tab === "overview" || tab === "stats",
  });
  const [apiRange, setApiRange] = useState("24h");
  const apiStats = useQuery({
    queryKey: ["broker-api-stats", id, apiRange],
    queryFn: ({ signal }) =>
      api<{ statistics: BrokerAPIStats }>(`/api/v1/admin/brokers/${id}/statistics?range=${apiRange}`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 15000),
    enabled: tab === "activity",
  });
  const brokerLogs = useQuery({
    queryKey: ["broker-logs", id],
    queryFn: ({ signal }) => api<{ logs: ObsLog[] }>(`/api/v1/admin/brokers/${id}/logs?limit=30`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 8000),
    enabled: tab === "logs",
  });
  const brokerErrors = useQuery({
    queryKey: ["broker-errors", id],
    queryFn: ({ signal }) => api<{ logs: ObsLog[] }>(`/api/v1/admin/brokers/${id}/logs?kind=errors&limit=8`, { signal }),
    refetchInterval: liveRefetchInterval(connected, 8000),
  });

  const emptyAccountForm = { trading_account: "", code: "", name: "", quik_firm: "" };
  const [accountForm, setAccountForm] = useState(emptyAccountForm);
  const [accountError, setAccountError] = useState("");
  const [accountListError, setAccountListError] = useState("");
  const [accountBusy, setAccountBusy] = useState(false);
  const [showAccountForm, setShowAccountForm] = useState(false);
  const [editingAccount, setEditingAccount] = useState<BrokerAccount | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [tokenName, setTokenName] = useState("Production API");
  const [quikFirm, setQuikFirm] = useState("");
  const [expiresIn, setExpiresIn] = useState(180);
  const [perms, setPerms] = useState<string[]>([...ALL_PERMS]);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealTitle, setRevealTitle] = useState("ТОКЕН СОЗДАН");
  const [revealNote, setRevealNote] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<APIToken | null>(null);
  const [editFirm, setEditFirm] = useState<APIToken | null>(null);
  const [editFirmValue, setEditFirmValue] = useState("");
  const [newIP, setNewIP] = useState("");
  const [newIPNote, setNewIPNote] = useState("");
  const [manageIP, setManageIP] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");

  const b = broker.data?.broker;
  const st = security.data?.security;
  const accountList: BrokerAccount[] = accounts.data?.accounts || [];
  const activeAccountCount = accountList.filter((a) => (a.status || "").toUpperCase() === "ACTIVE").length;
  const tokenList = tokens.data?.tokens || [];
  const ipEntries = whitelist.data?.entries || [];
  const wlEnabled = !!(whitelist.data?.ip_whitelist_enabled ?? b?.ip_whitelist_enabled);

  const crumbItems = useMemo(
    () => [{ label: "Брокеры", to: "/brokers" }, { label: b?.name || "Карточка брокера" }],
    [b?.name],
  );
  useBreadcrumbs(crumbItems);

  const activity = useMemo(() => {
    const bid = Number(id);
    return (audit.data?.audit || [])
      .filter((a) => a.broker_id == null || Number(a.broker_id) === bid)
      .slice(0, 6);
  }, [audit.data, id]);

  useEffect(() => {
    if (!TABS.some((t) => t.id === tab)) setParams({ tab: "tokens" }, { replace: true });
  }, [tab, setParams]);

  function setTab(next: TabId) {
    setParams({ tab: next });
  }

  function openAccountCreate() {
    setEditingAccount(null);
    setAccountForm(emptyAccountForm);
    setAccountError("");
    setShowAccountForm(true);
  }

  function openAccountEdit(a: BrokerAccount) {
    setEditingAccount(a);
    setAccountForm({
      trading_account: a.trading_account || "",
      code: a.code || "",
      name: a.name || "",
      quik_firm: a.quik_firm || "",
    });
    setAccountError("");
    setShowAccountForm(true);
  }

  async function saveAccount(e: FormEvent) {
    e.preventDefault();
    const trading = accountForm.trading_account.trim();
    const code = accountForm.code.trim() || trading;
    if (!trading && !code) {
      setAccountError("Укажите торговый счёт — его брокер передаёт в account_id заявки");
      return;
    }
    setAccountBusy(true);
    setAccountError("");
    try {
      const body = {
        trading_account: trading || code,
        code,
        name: accountForm.name.trim() || code,
        quik_firm: accountForm.quik_firm.trim(),
      };
      if (editingAccount) {
        await api(`/api/v1/admin/brokers/${id}/accounts/${editingAccount.id}`, {
          method: "PATCH",
          body: JSON.stringify(body),
        });
      } else {
        await api(`/api/v1/admin/brokers/${id}/accounts`, {
          method: "POST",
          body: JSON.stringify(body),
        });
      }
      setShowAccountForm(false);
      setEditingAccount(null);
      qc.invalidateQueries({ queryKey: ["accounts", id] });
    } catch (err) {
      setAccountError((err as Error).message || "Не удалось сохранить счёт");
    } finally {
      setAccountBusy(false);
    }
  }

  async function setAccountStatus(a: BrokerAccount, status: "ACTIVE" | "DISABLED") {
    setAccountListError("");
    try {
      await api(`/api/v1/admin/brokers/${id}/accounts/${a.id}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      });
      qc.invalidateQueries({ queryKey: ["accounts", id] });
    } catch (err) {
      setAccountListError((err as Error).message || "Не удалось изменить статус счёта");
    }
  }

  function openCreate() {
    setQuikFirm(b?.code ? String(b.code) : "");
    setShowCreate(true);
  }

  const createToken = useMutation({
    mutationFn: () =>
      api<{ token: string }>(`/api/v1/admin/brokers/${id}/tokens`, {
        method: "POST",
        body: JSON.stringify({
          name: tokenName,
          quik_firm: quikFirm.trim(),
          expires_in_days: expiresIn,
          permissions: perms,
        }),
      }),
    onSuccess: (res) => {
      setRevealedToken(res.token);
      setRevealTitle("ТОКЕН СОЗДАН");
      setRevealNote("Этот токен показывается только один раз. Скопируйте его сейчас.");
      setShowCreate(false);
      setQuikFirm("");
      qc.invalidateQueries({ queryKey: ["tokens", id] });
      qc.invalidateQueries({ queryKey: ["security", id] });
    },
  });

  async function rotate(tokenId: string) {
    const res = await api<{ token: string }>(`/api/v1/admin/tokens/${tokenId}/rotate`, { method: "POST" });
    setRevealedToken(res.token);
    setRevealTitle("ТОКЕН РОТИРОВАН");
    setRevealNote("Старый токен продолжает работать. После переключения брокера необходимо отозвать старый токен.");
    qc.invalidateQueries({ queryKey: ["tokens", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function revoke(tokenId: string) {
    await api(`/api/v1/admin/tokens/${tokenId}/revoke`, { method: "POST" });
    setConfirmRevoke(null);
    qc.invalidateQueries({ queryKey: ["tokens", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function saveFirm() {
    if (!editFirm) return;
    await api(`/api/v1/admin/tokens/${editFirm.id}`, {
      method: "PATCH",
      body: JSON.stringify({ quik_firm: editFirmValue.trim() }),
    });
    setEditFirm(null);
    qc.invalidateQueries({ queryKey: ["tokens", id] });
  }

  async function toggleWhitelist(enabled: boolean) {
    await api(`/api/v1/admin/brokers/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ ip_whitelist_enabled: enabled }),
    });
    qc.invalidateQueries({ queryKey: ["broker", id] });
    qc.invalidateQueries({ queryKey: ["ipwl", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function addIP(e: FormEvent) {
    e.preventDefault();
    await api(`/api/v1/admin/brokers/${id}/ip-whitelist`, {
      method: "POST",
      body: JSON.stringify({ ip: newIP, note: newIPNote }),
    });
    setNewIP("");
    setNewIPNote("");
    qc.invalidateQueries({ queryKey: ["ipwl", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function removeIP(entryId: number) {
    await api(`/api/v1/admin/brokers/${id}/ip-whitelist/${entryId}`, { method: "DELETE" });
    qc.invalidateQueries({ queryKey: ["ipwl", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  function togglePerm(p: string) {
    setPerms((prev) => (prev.includes(p) ? prev.filter((x) => x !== p) : [...prev, p]));
  }

  function expiresTone(expiresAt?: string) {
    if (!expiresAt) return "text-slate-600";
    const days = (new Date(expiresAt).getTime() - Date.now()) / 86400000;
    if (days < 0) return "text-danger font-medium";
    if (days < 30) return "text-warn font-medium";
    return "text-slate-600";
  }

  if (broker.isLoading) return <PageSpinner />;
  if (broker.error) return <ErrorBanner error={broker.error} onRetry={() => broker.refetch()} />;

  const ordersToday = brokerStats.data?.stats?.today?.orders ?? "—";

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">{b?.name}</h1>
            <StatusBadge status={b?.status} />
          </div>
          <p className="mt-1 text-sm text-slate-500">
            ID: {b?.id} · Создан: {formatDateTime(b?.created_at)}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <SummaryChip label="Токены" value={`${st?.active_tokens ?? 0} активных`} />
          <SummaryChip label="Счета" value={`${activeAccountCount} активных`} />
          <SummaryChip label="Заявки сегодня" value={String(ordersToday)} />
          <button
            type="button"
            className="btn gap-1.5"
            onClick={() => {
              setEditName(b?.name || "");
              setEditDesc(b?.description || "");
              setEditOpen(true);
            }}
          >
            <Icons.edit size={16} />
            Редактировать
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-slate-200">
        <div className="flex gap-1 overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={`whitespace-nowrap border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                tab === t.id
                  ? "border-accent text-accent"
                  : "border-transparent text-slate-500 hover:text-ink"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_280px]">
        <div className="space-y-4">
          {(tab === "tokens" || tab === "overview") && (
            <section className="card !p-0 overflow-hidden">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
                <div>
                  <h2 className="font-semibold">Токены</h2>
                  <p className="text-xs text-slate-500">Управление API-токенами брокера</p>
                </div>
                <button type="button" className="btn gap-1.5" onClick={openCreate}>
                  <Icons.plus size={16} />
                  Создать токен
                </button>
              </div>
              <div className="overflow-x-auto">
                <DataTable<APIToken> title="Токены брокера" rows={tokenList} columns={[{ id: "c0", header: "Название", value: (t) => t.name },
{ id: "c1", header: "Статус", value: (t) => t.status },
{ id: "c2", header: "Права", value: (t) => (t.permissions || []).join(" ") },
{ id: "c3", header: "Истекает", value: (t) => dateValue(t.expires_at) },
{ id: "c4", header: "Последнее использование", value: (t) => dateValue(t.last_used_at) },
{ id: "actions", header: "Действия", value: () => "", sortable: false }]} rowKey={row => row.id} renderRow={(t) => (
                      <tr key={t.id} className="align-middle">
                        <td>
                          <div className="font-medium">{t.name}</div>
                          <div className="font-mono text-[11px] text-slate-400">{t.token_prefix}…</div>
                        </td>
                        <td>
                          <StatusBadge status={t.status} />
                        </td>
                        <td>
                          <div className="flex items-center gap-1 text-slate-500">
                            {(t.permissions || []).slice(0, 6).map((p) => {
                              const Icon = PERM_ICON[p] || Icons.lock;
                              return (
                                <span key={p} title={p} className="rounded bg-slate-50 p-1 text-slate-500">
                                  <Icon size={13} />
                                </span>
                              );
                            })}
                          </div>
                        </td>
                        <td className={expiresTone(t.expires_at)}>
                          {t.expires_at ? formatDate(t.expires_at) : "Без срока"}
                        </td>
                        <td>
                          <div className="text-sm">{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</div>
                          {t.last_used_ip && <div className="font-mono text-[11px] text-slate-400">{t.last_used_ip}</div>}
                        </td>
                        <td className="relative">
                          {t.status === "ACTIVE" && (
                            <RowActions label={"Действия токена " + t.name} actions={[
                              { label: "Изменить фирму", run: () => { setEditFirm(t); setEditFirmValue(t.quik_firm || ""); } },
                              { label: "Ротировать токен", run: () => rotate(t.id) },
                              { label: "Отозвать токен", danger: true, run: () => setConfirmRevoke(t) },
                            ]} />
                          )}
                        </td>
                      </tr>
                    )} emptyTitle="Записей пока нет" />
              </div>
              <div className="m-4 flex gap-2 rounded-lg border border-sky-100 bg-sky-50 px-3 py-2.5 text-sm text-sky-900">
                <Icons.info size={18} className="mt-0.5 shrink-0 text-sky-600" />
                <p>
                  Полное значение токена показывается только при создании или ротации. Храните секреты в безопасном
                  хранилище — повторно получить значение нельзя.
                </p>
              </div>
            </section>
          )}

          {tab === "accounts" && (
            <section className="card !p-0 overflow-hidden">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
                <div>
                  <h2 className="font-semibold">Торговые счета</h2>
                  <p className="text-xs text-slate-500">
                    В заявке брокер передаёт <span className="font-mono">account_id</span> — он должен совпасть с торговым
                    счётом или кодом активного счёта (FIX tag 1)
                  </p>
                </div>
                <button type="button" className="btn gap-1.5" onClick={openAccountCreate}>
                  <Icons.plus size={16} />
                  Добавить счёт
                </button>
              </div>
              {accountListError && <p className="px-4 pt-3 text-sm text-danger">{accountListError}</p>}
              <div className="overflow-x-auto">
                <DataTable<BrokerAccount> title="Торговые счета" rows={accountList} columns={[{ id: "c0", header: "Торговый счёт", value: (a) => a.trading_account },
{ id: "c1", header: "Код", value: (a) => a.code },
{ id: "c2", header: "Название", value: (a) => a.name },
{ id: "c3", header: "Статус", value: (a) => a.status },
{ id: "actions", header: "Действия", value: () => "", sortable: false }]} rowKey={row => row.id} renderRow={(a) => {
                      const active = (a.status || "").toUpperCase() === "ACTIVE";
                      return (
                        <tr key={a.id} className="align-middle">
                          <td className="font-mono">{a.trading_account}</td>
                          <td className="font-mono text-slate-600">{a.code}</td>
                          <td>{a.name || "—"}</td>
                          <td>
                            <StatusBadge status={a.status} />
                          </td>
                          <td className="whitespace-nowrap">
                            <button type="button" className="btn-muted mr-2" onClick={() => openAccountEdit(a)}>
                              Изменить
                            </button>
                            {active ? (
                              <button
                                type="button"
                                className="btn-muted"
                                onClick={() => setAccountStatus(a, "DISABLED")}
                              >
                                Отключить
                              </button>
                            ) : (
                              <button
                                type="button"
                                className="btn-muted"
                                onClick={() => setAccountStatus(a, "ACTIVE")}
                              >
                                Включить
                              </button>
                            )}
                          </td>
                        </tr>
                      );
                    }} emptyTitle="Записей пока нет" />
              </div>
              <div className="m-4 flex gap-2 rounded-lg border border-sky-100 bg-sky-50 px-3 py-2.5 text-sm text-sky-900">
                <Icons.info size={18} className="mt-0.5 shrink-0 text-sky-600" />
                <p>
                  Отключённый счёт остаётся в справочнике, но заявки с его <span className="font-mono">account_id</span>{" "}
                  будут отклонены с 400.
                </p>
              </div>
            </section>
          )}

          {(tab === "ip" || tab === "tokens" || tab === "overview") && (
            <div className={`grid gap-4 ${tab === "ip" ? "" : "lg:grid-cols-2"}`}>
              <section className="card !p-0 overflow-hidden">
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <h2 className="font-semibold">IP Whitelist</h2>
                    <span className={wlEnabled ? "badge-success" : "badge-muted"}>
                      {wlEnabled ? "Включен" : "Выключен"}
                    </span>
                  </div>
                  <button type="button" className="btn-muted" onClick={() => setManageIP(true)}>
                    Управление IP
                  </button>
                </div>
                <DataTable<IPWhitelistEntry> title="IP Whitelist" rows={ipEntries} columns={[{ id: "c0", header: "IP", value: (e) => e.ip_cidr },
{ id: "c1", header: "Описание", value: (e) => e.note }]} rowKey={row => row.id} renderRow={(e) => (
                      <tr key={e.id}>
                        <td className="font-mono text-sm">{e.ip_cidr}</td>
                        <td className="text-slate-500">{e.note || "—"}</td>
                      </tr>
                    )} emptyTitle="Записей пока нет" />
              </section>

              {(tab === "tokens" || tab === "overview") && (
                <section className="card space-y-3">
                  <div className="flex items-center justify-between">
                    <h2 className="font-semibold">Статистика заявок</h2>
                    <select
                      className="rounded-md border border-line bg-panel px-2 py-1 text-xs text-muted"
                      value={statsDays}
                      onChange={(e) => setStatsDays(Number(e.target.value))}
                    >
                      <option value={7}>7 дней</option>
                      <option value={14}>14 дней</option>
                      <option value={30}>30 дней</option>
                    </select>
                  </div>
                  <RequestChart series={brokerStats.data?.stats?.series || []} />
                  <div className="grid grid-cols-3 gap-3 border-t border-slate-100 pt-3">
                    <Metric
                      label="Заявки"
                      value={String(brokerStats.data?.stats?.period?.orders ?? 0)}
                      delta={pctDelta(
                        brokerStats.data?.stats?.period?.orders,
                        brokerStats.data?.stats?.previous?.orders,
                      )}
                    />
                    <Metric
                      label="Исполнено"
                      value={String(brokerStats.data?.stats?.period?.filled ?? 0)}
                      delta={pctDelta(
                        brokerStats.data?.stats?.period?.filled,
                        brokerStats.data?.stats?.previous?.filled,
                      )}
                    />
                    <Metric
                      label="Ошибки"
                      value={String(brokerStats.data?.stats?.period?.rejected ?? 0)}
                      delta={pctDelta(
                        brokerStats.data?.stats?.period?.rejected,
                        brokerStats.data?.stats?.previous?.rejected,
                      )}
                    />
                  </div>
                </section>
              )}
            </div>
          )}

          {tab === "rights" && (
            <section className="card space-y-3">
              <h2 className="font-semibold">Права API</h2>
              <p className="text-sm text-slate-500">Права назначаются на уровне каждого токена при создании.</p>
              <div className="grid gap-2 sm:grid-cols-2">
                {ALL_PERMS.map((p) => (
                  <div key={p} className="flex items-center gap-2 rounded-lg border border-slate-100 bg-slate-50 px-3 py-2 text-sm">
                    {(PERM_ICON[p] || Icons.lock)({ size: 14 })}
                    <span className="font-mono">{p}</span>
                  </div>
                ))}
              </div>
            </section>
          )}

          {tab === "history" && (
            <section className="card space-y-3">
              <h2 className="font-semibold">История</h2>
              <ActivityList items={activity} empty="Нет событий" />
            </section>
          )}

          {tab === "activity" && (
            <BrokerActivity
              st={apiStats.data?.statistics}
              range={apiRange}
              onRange={setApiRange}
              errors={brokerErrors.data?.logs || []}
              loading={apiStats.isLoading}
            />
          )}

          {tab === "logs" && (
            <section className="card space-y-3">
              <div className="flex items-center justify-between">
                <h2 className="font-semibold">Логи фирмы</h2>
                <Link className="text-sm text-accent" to={`/logs?broker_id=${id}`}>
                  Открыть в разделе Логи →
                </Link>
              </div>
              <DataTable<ObsLog> title="События брокера" rows={(brokerLogs.data?.logs || [])} columns={[{ id: "c0", header: "Время", value: (row) => dateValue(row.timestamp) },
{ id: "c1", header: "Sev", value: (row) => row.severity },
{ id: "c2", header: "Тип", value: (row) => row.type },
{ id: "c3", header: "Событие", value: (row) => row.subtype },
{ id: "c4", header: "IP", value: (row) => row.client_ip }]} rowKey={row => row.id ?? ""} renderRow={(row) => (
                    <tr key={row.id}>
                      <td className="font-mono text-xs">{formatDateTime(row.timestamp)}</td>
                      <td>{row.severity}</td>
                      <td>{row.type}</td>
                      <td className="font-mono text-xs">{row.subtype}</td>
                      <td className="font-mono text-xs">{row.client_ip || "—"}</td>
                    </tr>
                  )} emptyTitle="Записей пока нет" />
            </section>
          )}

          {tab === "stats" && (
            <section className="card space-y-3">
              <h2 className="font-semibold">Сводка безопасности</h2>
              <div className="grid gap-3 sm:grid-cols-3">
                <SummaryChip label="Активные токены" value={String(st?.active_tokens ?? 0)} />
                <SummaryChip label="Отозванные" value={String(st?.revoked_tokens ?? 0)} />
                <SummaryChip label="Неудачные входы 24ч" value={String(st?.failed_auth_attempts_24h ?? 0)} />
              </div>
            </section>
          )}
        </div>

        {/* Right column */}
        <div className="space-y-4">
          <section className="card space-y-3">
            <h2 className="font-semibold">Детали брокера</h2>
            <dl className="space-y-2.5 text-sm">
              <DetailRow label="Название" value={b?.name} />
              <DetailRow label="Статус" value={<StatusBadge status={b?.status} />} />
              <DetailRow label="Код" value={<span className="font-mono">{b?.code}</span>} />
              <DetailRow label="Описание" value={b?.description || "—"} />
              <DetailRow label="Создан" value={formatDateTime(b?.created_at)} />
              <DetailRow label="Обновлён" value={formatDateTime(b?.updated_at)} />
            </dl>
          </section>

          <section className="card space-y-3">
            <h2 className="font-semibold">Последние ошибки</h2>
            {(brokerErrors.data?.logs || []).length === 0 ? (
              <p className="text-sm text-slate-400">Ошибок за выбранный период не обнаружено.</p>
            ) : (
              <ul className="space-y-2">
                {(brokerErrors.data?.logs || []).slice(0, 4).map((e) => (
                  <li key={e.id} className="text-sm">
                    <div className="font-medium text-danger">{e.error_code || e.subtype}</div>
                    <div className="text-[11px] text-slate-400">{formatTime(e.timestamp)}</div>
                  </li>
                ))}
              </ul>
            )}
            <button type="button" className="text-sm font-medium text-accent hover:underline" onClick={() => setTab("logs")}>
              Все логи фирмы →
            </button>
          </section>
        </div>
      </div>

      {/* Modals */}
      {manageIP && (
        <Modal title="Управление IP Whitelist" onClose={() => setManageIP(false)}>
          <label className="mb-3 flex items-center gap-2 text-sm">
            <input type="checkbox" checked={wlEnabled} onChange={(e) => toggleWhitelist(e.target.checked)} />
            Включить whitelist
          </label>
          <form onSubmit={addIP} className="mb-3 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
            <input
              className="input"
              placeholder="185.10.10.10 или CIDR"
              value={newIP}
              onChange={(e) => setNewIP(e.target.value)}
              required
            />
            <input
              className="input"
              placeholder="Описание"
              value={newIPNote}
              onChange={(e) => setNewIPNote(e.target.value)}
            />
            <button className="btn" type="submit">
              Добавить
            </button>
          </form>
          <DataTable<IPWhitelistEntry> title="Адреса доступа" rows={ipEntries} columns={[{ id: "c0", header: "IP / CIDR", value: (e) => e.ip_cidr },
{ id: "c1", header: "Описание", value: (e) => e.note },
{ id: "actions", header: "Действия", value: () => "", sortable: false }]} rowKey={row => row.id} renderRow={(e) => (
                <tr key={e.id}>
                  <td className="font-mono">{e.ip_cidr}</td>
                  <td>{e.note || "—"}</td>
                  <td>
                    <button type="button" className="btn-muted" onClick={() => removeIP(e.id)}>
                      Удалить
                    </button>
                  </td>
                </tr>
              )} emptyTitle="Записей пока нет" />
        </Modal>
      )}

      {showAccountForm && (
        <Modal
          title={editingAccount ? "Изменить счёт" : "Добавить торговый счёт"}
          onClose={() => {
            setShowAccountForm(false);
            setEditingAccount(null);
          }}
        >
          <form onSubmit={saveAccount} className="space-y-3">
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Торговый счёт (FIX tag 1)</span>
              <input
                className="input font-mono"
                placeholder="например 1-3101-2040"
                value={accountForm.trading_account}
                onChange={(e) => setAccountForm((f) => ({ ...f, trading_account: e.target.value }))}
                required
              />
              <span className="text-xs text-slate-400">
                Значение, которое брокер передаёт в <span className="font-mono">account_id</span> при создании заявки
              </span>
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Код</span>
              <input
                className="input font-mono"
                placeholder="по умолчанию = торговый счёт"
                value={accountForm.code}
                onChange={(e) => setAccountForm((f) => ({ ...f, code: e.target.value }))}
              />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Название</span>
              <input
                className="input"
                placeholder="необязательно"
                value={accountForm.name}
                onChange={(e) => setAccountForm((f) => ({ ...f, name: e.target.value }))}
              />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Код фирмы QUIK</span>
              <input
                className="input font-mono"
                placeholder="необязательно"
                value={accountForm.quik_firm}
                onChange={(e) => setAccountForm((f) => ({ ...f, quik_firm: e.target.value }))}
              />
            </label>
            {accountError && <p className="text-sm text-danger">{accountError}</p>}
            <div className="flex justify-end gap-2 pt-1">
              <button
                type="button"
                className="btn-muted"
                onClick={() => {
                  setShowAccountForm(false);
                  setEditingAccount(null);
                }}
              >
                Отмена
              </button>
              <button type="submit" className="btn" disabled={accountBusy}>
                {accountBusy ? "Сохранение…" : editingAccount ? "Сохранить" : "Добавить"}
              </button>
            </div>
          </form>
        </Modal>
      )}

      {showCreate && (
        <Modal title="Создать API-токен" onClose={() => setShowCreate(false)}>
          <label className="mb-3 block space-y-1 text-sm">
            <span className="text-slate-500">Название токена</span>
            <input className="input" value={tokenName} onChange={(e) => setTokenName(e.target.value)} />
          </label>
          <label className="mb-3 block space-y-1 text-sm">
            <span className="text-slate-500">Код фирмы (FIX 5018)</span>
            <input
              className="input font-mono"
              placeholder="например 2040"
              value={quikFirm}
              onChange={(e) => setQuikFirm(e.target.value)}
              required
            />
          </label>
          <label className="mb-3 block space-y-1 text-sm">
            <span className="text-slate-500">Срок действия</span>
            <select className="input" value={expiresIn} onChange={(e) => setExpiresIn(Number(e.target.value))}>
              <option value={30}>30 дней</option>
              <option value={90}>90 дней</option>
              <option value={180}>180 дней</option>
              <option value={365}>365 дней</option>
              <option value={0}>Без срока</option>
            </select>
          </label>
          <div className="mb-4 space-y-2">
            <div className="flex gap-2">
              <button type="button" className="btn-muted" onClick={() => setPerms([...ALL_PERMS])}>
                Полный доступ
              </button>
              <button type="button" className="btn-muted" onClick={() => setPerms([...READ_ONLY])}>
                Только чтение
              </button>
            </div>
            <div className="grid grid-cols-2 gap-2 text-sm">
              {ALL_PERMS.map((p) => (
                <label key={p} className="flex items-center gap-2">
                  <input type="checkbox" checked={perms.includes(p)} onChange={() => togglePerm(p)} />
                  <span className="font-mono">{p}</span>
                </label>
              ))}
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" className="btn-muted" onClick={() => setShowCreate(false)}>
              Отмена
            </button>
            <button
              type="button"
              className="btn"
              disabled={createToken.isPending || !tokenName || !quikFirm.trim() || perms.length === 0}
              onClick={() => createToken.mutate()}
            >
              Создать
            </button>
          </div>
        </Modal>
      )}

      {editFirm && (
        <Modal title="Код фирмы (FIX 5018)" onClose={() => setEditFirm(null)}>
          <p className="mb-3 text-sm text-slate-600">
            Токен <span className="font-medium">{editFirm.name}</span>. Значение уходит в NewOrderSingle tag 5018.
          </p>
          <input
            className="input mb-4 font-mono"
            value={editFirmValue}
            onChange={(e) => setEditFirmValue(e.target.value)}
            placeholder="код фирмы"
          />
          <div className="flex justify-end gap-2">
            <button type="button" className="btn-muted" onClick={() => setEditFirm(null)}>
              Отмена
            </button>
            <button type="button" className="btn" disabled={!editFirmValue.trim()} onClick={() => saveFirm()}>
              Сохранить
            </button>
          </div>
        </Modal>
      )}

      {revealedToken && (
        <Modal title={revealTitle} onClose={() => setRevealedToken(null)}>
          <p className="mb-3 text-sm text-slate-600">{revealNote}</p>
          <div className="mb-4 break-all rounded-lg border border-slate-200 bg-surface p-4 font-mono text-sm">
            {revealedToken}
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" className="btn" onClick={() => navigator.clipboard.writeText(revealedToken)}>
              Копировать токен
            </button>
            <button type="button" className="btn-muted" onClick={() => setRevealedToken(null)}>
              Закрыть
            </button>
          </div>
        </Modal>
      )}

      {confirmRevoke && (
        <Modal title="Отозвать токен?" onClose={() => setConfirmRevoke(null)}>
          <p className="mb-4 text-sm text-slate-600">
            <span className="font-medium">{confirmRevoke.name}</span> перестанет работать сразу. Любой запрос с этим
            токеном получит HTTP 401.
          </p>
          <div className="flex justify-end gap-2">
            <button type="button" className="btn-muted" onClick={() => setConfirmRevoke(null)}>
              Отмена
            </button>
            <button type="button" className="btn" onClick={() => revoke(confirmRevoke.id)}>
              Отозвать
            </button>
          </div>
        </Modal>
      )}

      {editOpen && (
        <Modal title="Редактировать брокера" onClose={() => setEditOpen(false)}>
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              await api(`/api/v1/admin/brokers/${id}`, {
                method: "PATCH",
                body: JSON.stringify({ name: editName.trim(), description: editDesc.trim() }),
              });
              setEditOpen(false);
              qc.invalidateQueries({ queryKey: ["broker", id] });
              qc.invalidateQueries({ queryKey: ["brokers"] });
            }}
          >
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Название</span>
              <input className="input" value={editName} onChange={(e) => setEditName(e.target.value)} required />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Описание</span>
              <textarea className="input min-h-[80px]" value={editDesc} onChange={(e) => setEditDesc(e.target.value)} />
            </label>
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-muted" onClick={() => setEditOpen(false)}>
                Отмена
              </button>
              <button type="submit" className="btn">
                Сохранить
              </button>
            </div>
          </form>
        </Modal>
      )}
    </div>
  );
}
