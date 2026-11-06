import { RowActions } from "../components/RowActions";
import { ActionButton } from "../components/ActionButton";
import { useConfirm } from "../components/confirm";
import { useToast } from "../components/Toast";
import { DataTable } from "../components/DataTable";
import { dateValue } from "../lib/table";
import { useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, apiWithHeaders } from "../api";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawColumns, rawSearchText } from "../components/rawColumns";
import { FirmSelect } from "../components/FirmSelect";
import { formatDate, formatDateTime, formatTime } from "../labels";
import { liveRefetchInterval } from "../lib/poll";
import { useAdminStream } from "../hooks/useAdminStream";
import { Icons } from "../components/icons";
import { StatusBadge } from "../components/StatusBadge";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { Modal } from "../components/Modal";
import { ErrorBanner, PageSpinner } from "../components/feedback";
import { ALL_PERMS, READ_ONLY_PERMS, type APIToken, type AuditRow, type Broker, type IPWhitelistEntry, type ObsLog } from "../types/admin";
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
import { BrokerEditForm, IPEntryForm } from "./broker/forms";
import type { IPEntryValues } from "../lib/schemas";

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
  // Trading accounts of the broker's token firms, rows of QUIK "Trdaccs" as stored.
  const accounts = useQuery({
    queryKey: ["accounts", id],
    queryFn: async ({ signal }) => {
      const { data, headers } = await apiWithHeaders<DBOrder[]>(`/api/v1/admin/brokers/${id}/accounts`, { signal }, parseDBJSON);
      return { rows: data ?? [], firms: (headers.get("X-Firms") || "").split(",").filter(Boolean) };
    },
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

  const [showCreate, setShowCreate] = useState(false);
  const [tokenName, setTokenName] = useState("Production API");
  const [quikFirm, setQuikFirm] = useState("");
  const [expiresIn, setExpiresIn] = useState(180);
  const [perms, setPerms] = useState<string[]>([...ALL_PERMS]);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealTitle, setRevealTitle] = useState("ТОКЕН СОЗДАН");
  const [revealNote, setRevealNote] = useState("");
  const [firmSaving, setFirmSaving] = useState(false);
  const toast = useToast();
  const confirm = useConfirm();
  const [editFirm, setEditFirm] = useState<APIToken | null>(null);
  const [editFirmValue, setEditFirmValue] = useState("");
  const [editFirmError, setEditFirmError] = useState("");

  const [manageIP, setManageIP] = useState(false);
  const [editOpen, setEditOpen] = useState(false);


  const b = broker.data?.broker;
  const st = security.data?.security;
  const accountList = accounts.data?.rows ?? [];
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
    qc.invalidateQueries({ queryKey: ["tokens", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function saveFirm() {
    if (!editFirm) return;
    setEditFirmError("");
    setFirmSaving(true);
    try {
      await api(`/api/v1/admin/tokens/${editFirm.id}`, {
        method: "PATCH",
        body: JSON.stringify({ quik_firm: editFirmValue.trim() }),
      });
    } catch (err) {
      setEditFirmError((err as Error).message || "Не удалось изменить фирму");
      return;
    } finally {
      setFirmSaving(false);
    }
    toast.push(`Фирма токена «${editFirm.name}» изменена`, "ok");
    setEditFirm(null);
    qc.invalidateQueries({ queryKey: ["tokens", id] });
    qc.invalidateQueries({ queryKey: ["accounts", id] });
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

  async function addIP({ ip, note }: IPEntryValues) {
    await api(`/api/v1/admin/brokers/${id}/ip-whitelist`, {
      method: "POST",
      body: JSON.stringify({ ip, note }),
    });
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
    if (!expiresAt) return "text-muted";
    const days = (new Date(expiresAt).getTime() - Date.now()) / 86400000;
    if (days < 0) return "text-danger font-medium";
    if (days < 30) return "text-warn font-medium";
    return "text-muted";
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
          <p className="mt-1 text-sm text-muted">
            ID: {b?.id} · Создан: {formatDateTime(b?.created_at)}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <SummaryChip label="Токены" value={`${st?.active_tokens ?? 0} активных`} />
          <SummaryChip label="Счета QUIK" value={accounts.data ? String(accountList.length) : "…"} />
          <SummaryChip label="Заявки сегодня" value={String(ordersToday)} />
          <button
            type="button"
            className="btn gap-1.5"
            onClick={() => {

              setEditOpen(true);
            }}
          >
            <Icons.edit size={16} />
            Редактировать
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-line">
        <div className="flex gap-1 overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={`whitespace-nowrap border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                tab === t.id
                  ? "border-accent text-accent"
                  : "border-transparent text-muted hover:text-ink"
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
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3">
                <div>
                  <h2 className="font-semibold">Токены</h2>
                  <p className="text-xs text-muted">Управление API-токенами брокера</p>
                </div>
                <button type="button" className="btn gap-1.5" onClick={openCreate}>
                  <Icons.plus size={16} />
                  Создать токен
                </button>
              </div>
              <div className="overflow-x-auto">
                <DataTable<APIToken> title="Токены брокера" rows={tokenList} rowKey={row => row.id} emptyTitle="Записей пока нет"
                  selection={{
                    canSelect: (t) => t.status === "ACTIVE",
                    actions: [{
                      label: "Отозвать выбранные", danger: true,
                      run: async (list) => {
                        const failed: string[] = [];
                        for (const t of list) { try { await revoke(t.id); } catch { failed.push(t.name); } }
                        if (failed.length) throw new Error(`Не удалось отозвать: ${failed.join(", ")}. Остальные отозваны.`);
                      },
                      confirm: (list) => ({ title: `Отозвать токены: ${list.length}?`, danger: true, confirmLabel: "Отозвать все", description: <>Перестанут работать сразу: <span className="font-medium text-ink">{list.map(t => t.name).join(", ")}</span>. Любой запрос с ними получит HTTP 401.</> }),
                      success: (list) => `Отозвано токенов: ${list.length}`,
                    }],
                  }}
                  columns={[
                    { id: "c0", header: "Название", value: (t) => t.name, cell: (t) => <><div className="font-medium">{t.name}</div><div className="font-mono text-[11px] text-faint">{t.token_prefix}…</div></> },
                    { id: "c1", header: "Статус", value: (t) => t.status, cell: (t) => <StatusBadge status={t.status} /> },
                    { id: "c2", header: "Права", value: (t) => (t.permissions || []).join(" "), cell: (t) => <div className="flex items-center gap-1 text-muted">{(t.permissions || []).slice(0, 6).map((p) => { const Icon = PERM_ICON[p] || Icons.lock; return <span key={p} title={p} className="rounded bg-subtle p-1 text-muted"><Icon size={13} /></span>; })}</div> },
                    { id: "c3", header: "Истекает", value: (t) => dateValue(t.expires_at), cell: (t) => <span className={expiresTone(t.expires_at)}>{t.expires_at ? formatDate(t.expires_at) : "Без срока"}</span> },
                    { id: "c4", header: "Последнее использование", value: (t) => dateValue(t.last_used_at), cell: (t) => <><div className="text-sm">{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</div>{t.last_used_ip && <div className="font-mono text-[11px] text-faint">{t.last_used_ip}</div>}</> },
                    { id: "actions", header: "Действия", value: () => "", sortable: false, cell: (t) => t.status === "ACTIVE" ? (
                      <RowActions label={"Действия токена " + t.name} actions={[
                        { label: "Изменить фирму", run: () => { setEditFirm(t); setEditFirmValue(t.quik_firm || ""); setEditFirmError(""); } },
                        { label: "Ротировать токен", run: () => rotate(t.id), success: `Токен «${t.name}» ротирован`, confirm: { title: "Ротировать токен?", description: "Будет выпущен новый токен. Старый продолжит работать, пока вы его не отзовёте.", confirmLabel: "Ротировать" } },
                        { label: "Отозвать токен", danger: true, run: () => revoke(t.id), success: `Токен «${t.name}» отозван`, confirm: { title: "Отозвать токен?", danger: true, confirmLabel: "Отозвать", description: <><span className="font-medium text-ink">{t.name}</span> перестанет работать сразу. Любой запрос с этим токеном получит HTTP 401.</> } },
                      ]} />
                    ) : null },
                  ]} />
              </div>
              <div className="m-4 flex gap-2 rounded-lg border border-info/20 bg-info-soft px-3 py-2.5 text-sm text-info">
                <Icons.info size={18} className="mt-0.5 shrink-0 text-info" />
                <p>
                  Полное значение токена показывается только при создании или ротации. Храните секреты в безопасном
                  хранилище — повторно получить значение нельзя.
                </p>
              </div>
            </section>
          )}

          {tab === "accounts" && (
            <section className="card !p-0 overflow-hidden">
              <div className="border-b border-line px-4 py-3">
                <h2 className="font-semibold">Торговые счета QUIK</h2>
                <p className="text-xs text-muted">
                  Строки таблицы QUIK Trdaccs фирм, указанных в активных токенах брокера
                  {accounts.data?.firms.length ? <> (<span className="font-mono">{accounts.data.firms.join(", ")}</span>)</> : null}.
                  В заявке брокер передаёт <span className="font-mono">account_id</span> — он должен совпасть с{" "}
                  <span className="font-mono">TrdAccId</span> фирмы своего токена. Счета заводятся в QUIK, не в Gateway.
                </p>
              </div>
              {accounts.error && <div className="px-4 pt-3"><ErrorBanner error={accounts.error} onRetry={() => accounts.refetch()} /></div>}
              <div className="overflow-x-auto">
                <DataTable title="Торговые счета QUIK" rows={accounts.error ? [] : accountList} columns={rawColumns(accountList)}
                  rowKey={(r) => `${dbValue(r.FirmId)}\u0000${dbValue(r.TrdAccId)}`} getSearchText={rawSearchText(accountList)}
                  loading={accounts.isLoading} exportName="trdaccs" showTotals={false}
                  emptyTitle={accounts.error ? "Таблица Trdaccs недоступна" : accounts.data?.firms.length ? "В Trdaccs нет счетов этих фирм" : "У брокера нет активных токенов с фирмой"} />
              </div>
            </section>
          )}

          {(tab === "ip" || tab === "tokens" || tab === "overview") && (
            <div className={`grid gap-4 ${tab === "ip" ? "" : "lg:grid-cols-2"}`}>
              <section className="card !p-0 overflow-hidden">
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-4 py-3">
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
                        <td className="text-muted">{e.note || "—"}</td>
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
                  <div className="grid grid-cols-3 gap-3 border-t border-line pt-3">
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
              <p className="text-sm text-muted">Права назначаются на уровне каждого токена при создании.</p>
              <div className="grid gap-2 sm:grid-cols-2">
                {ALL_PERMS.map((p) => (
                  <div key={p} className="flex items-center gap-2 rounded-lg border border-line bg-subtle px-3 py-2 text-sm">
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
              <p className="text-sm text-faint">Ошибок за выбранный период не обнаружено.</p>
            ) : (
              <ul className="space-y-2">
                {(brokerErrors.data?.logs || []).slice(0, 4).map((e) => (
                  <li key={e.id} className="text-sm">
                    <div className="font-medium text-danger">{e.error_code || e.subtype}</div>
                    <div className="text-[11px] text-faint">{formatTime(e.timestamp)}</div>
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
            <input type="checkbox" checked={wlEnabled} onChange={(e) => {
              const enabled = e.target.checked;
              void confirm({
                title: enabled ? "Включить IP whitelist?" : "Выключить IP whitelist?",
                description: enabled
                  ? ipEntries.length ? "Запросы брокера будут приниматься только с адресов из списка." : "Список пуст: после включения брокер не сможет обратиться к API ни с одного адреса."
                  : "Запросы брокера будут приниматься с любого адреса.",
                confirmLabel: enabled ? "Включить" : "Выключить",
                danger: enabled ? !ipEntries.length : true,
                action: () => toggleWhitelist(enabled),
                success: enabled ? "IP whitelist включён" : "IP whitelist выключен",
              });
            }} />
            Включить whitelist
          </label>
          <IPEntryForm onAdd={async (values) => { await addIP(values); toast.push(`Адрес ${values.ip} добавлен`, "ok"); }} />
          <DataTable<IPWhitelistEntry> title="Адреса доступа" rows={ipEntries} rowKey={row => row.id} emptyTitle="Записей пока нет"
            selection={{ actions: [{
              label: "Удалить выбранные", danger: true,
              run: async (list) => { for (const e of list) await removeIP(e.id); },
              confirm: (list) => ({ title: `Удалить адреса: ${list.length}?`, danger: true, confirmLabel: "Удалить", description: `${list.map(e => e.ip_cidr).join(", ")} больше не смогут обращаться к API, пока whitelist включён.` }),
              success: (list) => `Удалено адресов: ${list.length}`,
            }] }}
            columns={[
              { id: "c0", header: "IP / CIDR", value: (e) => e.ip_cidr, className: "font-mono" },
              { id: "c1", header: "Описание", value: (e) => e.note, cell: (e) => e.note || "—" },
              { id: "actions", header: "Действия", value: () => "", sortable: false, cell: (e) => (
                <ActionButton onAction={() => removeIP(e.id)} success={`Адрес ${e.ip_cidr} удалён`} confirm={{ title: "Удалить адрес?", description: `${e.ip_cidr} больше не сможет обращаться к API, пока whitelist включён.`, confirmLabel: "Удалить", danger: true }}>
                  Удалить
                </ActionButton>
              ) },
            ]} />
        </Modal>
      )}

      {showCreate && (
        <Modal title="Создать API-токен" onClose={() => setShowCreate(false)}>
          <label className="mb-3 block space-y-1 text-sm">
            <span className="text-muted">Название токена</span>
            <input className="input" value={tokenName} onChange={(e) => setTokenName(e.target.value)} />
          </label>
          <FirmSelect value={quikFirm} onChange={setQuikFirm} />
          <label className="mb-3 block space-y-1 text-sm">
            <span className="text-muted">Срок действия</span>
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
          {createToken.error && <p className="mb-3 text-sm text-danger">{(createToken.error as Error).message}</p>}
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
              {createToken.isPending && <Icons.refresh size={14} className="animate-spin" />}
              {createToken.isPending ? "Создание…" : "Создать"}
            </button>
          </div>
        </Modal>
      )}

      {editFirm && (
        <Modal title="Код фирмы (FIX 5018)" onClose={() => setEditFirm(null)}>
          <p className="mb-3 text-sm text-muted">
            Токен <span className="font-medium">{editFirm.name}</span>. Значение уходит в NewOrderSingle tag 5018.
          </p>
          <FirmSelect value={editFirmValue} onChange={setEditFirmValue} />
          {editFirmError && <p className="mb-3 text-sm text-danger">{editFirmError}</p>}
          <div className="flex justify-end gap-2">
            <button type="button" className="btn-muted" onClick={() => setEditFirm(null)}>
              Отмена
            </button>
            <button type="button" className="btn" disabled={!editFirmValue.trim() || firmSaving} onClick={() => saveFirm()}>
              {firmSaving && <Icons.refresh size={14} className="animate-spin" />}
              {firmSaving ? "Сохранение…" : "Сохранить"}
            </button>
          </div>
        </Modal>
      )}

      {revealedToken && (
        <Modal title={revealTitle} onClose={() => setRevealedToken(null)}>
          <p className="mb-3 text-sm text-muted">{revealNote}</p>
          <div className="mb-4 break-all rounded-lg border border-line bg-surface p-4 font-mono text-sm">
            {revealedToken}
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" className="btn" onClick={() => navigator.clipboard.writeText(revealedToken).then(() => toast.push("Токен скопирован в буфер обмена", "ok"), () => toast.push("Браузер не дал скопировать: выделите токен вручную"))}>
              Копировать токен
            </button>
            <button type="button" className="btn-muted" onClick={() => setRevealedToken(null)}>
              Закрыть
            </button>
          </div>
        </Modal>
      )}


      {editOpen && (
        <Modal title="Редактировать брокера" onClose={() => setEditOpen(false)}>
          <BrokerEditForm
            initial={{ name: b?.name || "", description: b?.description || "" }}
            onCancel={() => setEditOpen(false)}
            onSave={async (values) => {
              await api(`/api/v1/admin/brokers/${id}`, { method: "PATCH", body: JSON.stringify(values) });
              setEditOpen(false);
              toast.push("Изменения брокера сохранены", "ok");
              qc.invalidateQueries({ queryKey: ["broker", id] });
              qc.invalidateQueries({ queryKey: ["brokers"] });
            }}
          />
        </Modal>
      )}
    </div>
  );
}
