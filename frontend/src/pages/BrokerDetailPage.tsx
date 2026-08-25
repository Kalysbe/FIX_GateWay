import { FormEvent, useEffect, useMemo, useState, type ReactElement, type ReactNode } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { formatDate, formatDateTime, formatMs, formatNumber, formatPct, formatTime } from "../labels";
import { Icons } from "../components/icons";
import { StatusBadge } from "../components/StatusBadge";
import { useBreadcrumbs } from "../components/breadcrumbs";

const ALL_PERMS = [
  "orders:create",
  "orders:read",
  "orders:cancel",
  "orders:replace",
  "trades:read",
  "positions:read",
] as const;

const READ_ONLY = ["orders:read", "trades:read", "positions:read"];

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

type Token = {
  id: string;
  name: string;
  description?: string;
  status: string;
  token_prefix: string;
  quik_firm?: string;
  permissions: string[];
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  last_used_ip?: string;
};

type AuditRow = {
  id: number;
  action: string;
  result?: string | null;
  error?: string | null;
  ip?: string | null;
  broker_id?: number | null;
  created_at: string;
};

type BrokerAPIStats = {
  today: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  week: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  month: { requests: number; successful: number; errors: number; success_rate: number; avg_latency_ms: number; p95_latency_ms: number; p99_latency_ms: number };
  last_activity?: string;
  top_errors?: Array<{ error: string; count: number; last_occurrence: string }>;
  series?: Array<{ bucket: string; requests: number; successful: number; errors: number }>;
};

type ObsLog = {
  id: string;
  timestamp: string;
  type: string;
  subtype: string;
  severity: string;
  error_code?: string;
  message?: string;
  client_ip?: string;
  endpoint?: string;
};

type BrokerStatsDTO = {
  days: number;
  today: { orders: number; filled: number; rejected: number; active: number };
  period: { orders: number; filled: number; rejected: number; active: number };
  previous: { orders: number; filled: number; rejected: number; active: number };
  series: Array<{ date: string; orders: number }>;
};

const PERM_ICON: Record<string, (p: { size?: number; className?: string }) => ReactElement> = {
  "orders:create": Icons.cart,
  "orders:read": Icons.eye,
  "orders:cancel": Icons.x,
  "orders:replace": Icons.refresh,
  "trades:read": Icons.book,
  "positions:read": Icons.chart,
};

export default function BrokerDetailPage() {
  const { id } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as TabId) || "tokens";
  const qc = useQueryClient();

  const broker = useQuery({ queryKey: ["broker", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}`) });
  const accounts = useQuery({ queryKey: ["accounts", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/accounts`) });
  const security = useQuery({ queryKey: ["security", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/security`) });
  const tokens = useQuery({ queryKey: ["tokens", id], queryFn: () => api<{ tokens: Token[] }>(`/api/v1/admin/brokers/${id}/tokens`) });
  const whitelist = useQuery({ queryKey: ["ipwl", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/ip-whitelist`) });
  const audit = useQuery({ queryKey: ["audit"], queryFn: () => api<{ audit: AuditRow[] }>("/api/v1/admin/audit") });
  const [statsDays, setStatsDays] = useState(7);
  const brokerStats = useQuery({
    queryKey: ["broker-stats", id, statsDays],
    queryFn: () => api<{ stats: BrokerStatsDTO }>(`/api/v1/admin/brokers/${id}/stats?days=${statsDays}`),
    refetchInterval: 15000,
  });
  const [apiRange, setApiRange] = useState("24h");
  const apiStats = useQuery({
    queryKey: ["broker-api-stats", id, apiRange],
    queryFn: () => api<{ statistics: BrokerAPIStats }>(`/api/v1/admin/brokers/${id}/statistics?range=${apiRange}`),
    refetchInterval: 15000,
  });
  const brokerLogs = useQuery({
    queryKey: ["broker-logs", id],
    queryFn: () => api<{ logs: ObsLog[] }>(`/api/v1/admin/brokers/${id}/logs?limit=30`),
    refetchInterval: 8000,
  });
  const brokerErrors = useQuery({
    queryKey: ["broker-errors", id],
    queryFn: () => api<{ logs: ObsLog[] }>(`/api/v1/admin/brokers/${id}/logs?kind=errors&limit=8`),
    refetchInterval: 8000,
  });

  const [accCode, setAccCode] = useState("001");
  const [tradingAccount, setTradingAccount] = useState("L01+00000F00");
  const [showCreate, setShowCreate] = useState(false);
  const [tokenName, setTokenName] = useState("Production API");
  const [quikFirm, setQuikFirm] = useState("");
  const [expiresIn, setExpiresIn] = useState(180);
  const [perms, setPerms] = useState<string[]>([...ALL_PERMS]);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealTitle, setRevealTitle] = useState("ТОКЕН СОЗДАН");
  const [revealNote, setRevealNote] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<Token | null>(null);
  const [menuToken, setMenuToken] = useState<string | null>(null);
  const [editFirm, setEditFirm] = useState<Token | null>(null);
  const [editFirmValue, setEditFirmValue] = useState("");
  const [newIP, setNewIP] = useState("");
  const [newIPNote, setNewIPNote] = useState("");
  const [manageIP, setManageIP] = useState(false);

  const b = broker.data?.broker;
  const st = security.data?.security;
  const accountList = accounts.data?.accounts || [];
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

  async function createAccount(e: FormEvent) {
    e.preventDefault();
    await api(`/api/v1/admin/brokers/${id}/accounts`, {
      method: "POST",
      body: JSON.stringify({ code: accCode, name: accCode, quik_firm: "FIRM", trading_account: tradingAccount }),
    });
    qc.invalidateQueries({ queryKey: ["accounts", id] });
  }

  function openCreate() {
    setQuikFirm(b?.code ? String(b.code) : "");
    setShowCreate(true);
  }

  const createToken = useMutation({
    mutationFn: () =>
      api<any>(`/api/v1/admin/brokers/${id}/tokens`, {
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
    const res = await api<any>(`/api/v1/admin/tokens/${tokenId}/rotate`, { method: "POST" });
    setRevealedToken(res.token);
    setRevealTitle("ТОКЕН РОТИРОВАН");
    setRevealNote("Старый токен продолжает работать. После переключения брокера необходимо отозвать старый токен.");
    setMenuToken(null);
    qc.invalidateQueries({ queryKey: ["tokens", id] });
    qc.invalidateQueries({ queryKey: ["security", id] });
  }

  async function revoke(tokenId: string) {
    await api(`/api/v1/admin/tokens/${tokenId}/revoke`, { method: "POST" });
    setConfirmRevoke(null);
    setMenuToken(null);
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

  if (broker.isLoading) return <div className="text-slate-500">Загрузка…</div>;
  if (broker.error) return <div className="text-danger">{(broker.error as Error).message}</div>;

  const ordersToday = brokerStats.data?.stats?.today?.orders ?? "—";

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">{b?.name}</h1>
            <StatusBadge status={b?.status} />
            <button type="button" className="icon-btn text-slate-400 hover:text-warn" title="В избранное">
              <Icons.star size={18} />
            </button>
          </div>
          <p className="mt-1 text-sm text-slate-500">
            ID: {b?.id} · Создан: {formatDateTime(b?.created_at)}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <SummaryChip label="Токены" value={`${st?.active_tokens ?? 0} активных`} />
          <SummaryChip label="Счета" value={String(accountList.length)} />
          <SummaryChip label="Заявки сегодня" value={String(ordersToday)} />
          <button type="button" className="btn gap-1.5" onClick={() => setTab("overview")}>
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
                <table className="table">
                  <thead>
                    <tr>
                      <th>Название</th>
                      <th>Статус</th>
                      <th>Права</th>
                      <th>Истекает</th>
                      <th>Последнее использование</th>
                      <th className="w-10"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {tokenList.map((t) => (
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
                            <>
                              <button
                                type="button"
                                className="icon-btn"
                                onClick={() => setMenuToken(menuToken === t.id ? null : t.id)}
                              >
                                <Icons.more size={16} />
                              </button>
                              {menuToken === t.id && (
                                <div className="absolute right-2 z-10 mt-1 w-40 rounded-lg border border-slate-200 bg-white py-1 shadow-lg">
                                  <button
                                    type="button"
                                    className="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50"
                                    onClick={() => {
                                      setEditFirm(t);
                                      setEditFirmValue(t.quik_firm || "");
                                      setMenuToken(null);
                                    }}
                                  >
                                    Фирма (5018)
                                  </button>
                                  <button
                                    type="button"
                                    className="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50"
                                    onClick={() => rotate(t.id)}
                                  >
                                    Ротация
                                  </button>
                                  <button
                                    type="button"
                                    className="block w-full px-3 py-1.5 text-left text-sm text-danger hover:bg-slate-50"
                                    onClick={() => {
                                      setConfirmRevoke(t);
                                      setMenuToken(null);
                                    }}
                                  >
                                    Отозвать
                                  </button>
                                </div>
                              )}
                            </>
                          )}
                        </td>
                      </tr>
                    ))}
                    {tokenList.length === 0 && (
                      <tr>
                        <td colSpan={6} className="py-8 text-center text-slate-400">
                          Токенов пока нет
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
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
            <section className="card space-y-3">
              <h2 className="font-semibold">Торговые счета</h2>
              <form onSubmit={createAccount} className="grid gap-2 md:grid-cols-3">
                <input className="input" placeholder="Код" value={accCode} onChange={(e) => setAccCode(e.target.value)} />
                <input
                  className="input"
                  placeholder="Торговый счёт"
                  value={tradingAccount}
                  onChange={(e) => setTradingAccount(e.target.value)}
                />
                <button className="btn" type="submit">
                  Добавить счёт
                </button>
              </form>
              <table className="table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Код</th>
                    <th>Торговый счёт</th>
                    <th>Статус</th>
                  </tr>
                </thead>
                <tbody>
                  {accountList.map((a: any) => (
                    <tr key={a.ID || a.id}>
                      <td>{a.ID || a.id}</td>
                      <td>{a.Code || a.code}</td>
                      <td className="font-mono">{a.TradingAccount || a.trading_account}</td>
                      <td>
                        <StatusBadge status={a.Status || a.status} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
                <table className="table">
                  <thead>
                    <tr>
                      <th>IP</th>
                      <th>Описание</th>
                    </tr>
                  </thead>
                  <tbody>
                    {ipEntries.slice(0, tab === "ip" ? 50 : 5).map((e: any) => (
                      <tr key={e.id}>
                        <td className="font-mono text-sm">{e.ip_cidr}</td>
                        <td className="text-slate-500">{e.note || "—"}</td>
                      </tr>
                    ))}
                    {ipEntries.length === 0 && (
                      <tr>
                        <td colSpan={2} className="py-6 text-center text-slate-400">
                          Список пуст
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </section>

              {(tab === "tokens" || tab === "overview") && (
                <section className="card space-y-3">
                  <div className="flex items-center justify-between">
                    <h2 className="font-semibold">Статистика заявок</h2>
                    <select
                      className="rounded-md border border-slate-200 bg-white px-2 py-1 text-xs text-slate-600"
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
              <table className="table">
                <thead>
                  <tr>
                    <th>Время</th>
                    <th>Sev</th>
                    <th>Тип</th>
                    <th>Событие</th>
                    <th>IP</th>
                  </tr>
                </thead>
                <tbody>
                  {(brokerLogs.data?.logs || []).map((row) => (
                    <tr key={row.id}>
                      <td className="font-mono text-xs">{formatDateTime(row.timestamp)}</td>
                      <td>{row.severity}</td>
                      <td>{row.type}</td>
                      <td className="font-mono text-xs">{row.subtype}</td>
                      <td className="font-mono text-xs">{row.client_ip || "—"}</td>
                    </tr>
                  ))}
                  {(brokerLogs.data?.logs || []).length === 0 && (
                    <tr>
                      <td colSpan={5} className="py-8 text-center text-slate-400">
                        Нет событий этой фирмы
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
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
              <DetailRow label="Email" value="—" />
              <DetailRow label="Телефон" value="—" />
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
          <table className="table">
            <thead>
              <tr>
                <th>IP / CIDR</th>
                <th>Описание</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {ipEntries.map((e: any) => (
                <tr key={e.id}>
                  <td className="font-mono">{e.ip_cidr}</td>
                  <td>{e.note || "—"}</td>
                  <td>
                    <button type="button" className="btn-muted" onClick={() => removeIP(e.id)}>
                      Удалить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
    </div>
  );
}

function SummaryChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[110px] rounded-xl border border-slate-200 bg-white px-3 py-2 shadow-sm">
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="text-sm font-semibold">{value}</div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-slate-500">{label}</dt>
      <dd className="text-right font-medium text-ink">{value ?? "—"}</dd>
    </div>
  );
}

function pctDelta(current?: number, previous?: number): { text: string; up: boolean | null } {
  const cur = Number(current ?? 0);
  const prev = Number(previous ?? 0);
  if (prev === 0 && cur === 0) return { text: "0%", up: null };
  if (prev === 0) return { text: "+100%", up: true };
  const pct = ((cur - prev) / prev) * 100;
  const rounded = Math.round(pct * 10) / 10;
  const sign = rounded > 0 ? "+" : "";
  return { text: `${sign}${rounded}%`, up: rounded === 0 ? null : rounded > 0 };
}

function Metric({
  label,
  value,
  delta,
}: {
  label: string;
  value: string;
  delta: { text: string; up: boolean | null };
}) {
  const tone =
    delta.up === true ? "text-emerald-600" : delta.up === false ? "text-danger" : "text-slate-400";
  return (
    <div>
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="text-base font-semibold">{value}</div>
      <div className={`text-xs font-medium ${tone}`}>{delta.text} к пред. периоду</div>
    </div>
  );
}

function RequestChart({ series }: { series: Array<{ date: string; orders: number }> }) {
  const points = series.length ? series.map((s) => Number(s.orders) || 0) : [0];
  const w = 320;
  const h = 120;
  const max = Math.max(...points, 1);
  const step = points.length > 1 ? w / (points.length - 1) : w;
  const coords = points.map((p, i) => `${i * step},${h - (p / max) * (h - 16) - 8}`).join(" ");
  const area = `0,${h} ${coords} ${points.length > 1 ? w : 0},${h}`;
  const empty = points.every((p) => p === 0);
  return (
    <div className="relative">
      <svg viewBox={`0 0 ${w} ${h}`} className="h-32 w-full" preserveAspectRatio="none">
        <defs>
          <linearGradient id="reqFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#57b6c0" stopOpacity="0.35" />
            <stop offset="100%" stopColor="#57b6c0" stopOpacity="0.02" />
          </linearGradient>
        </defs>
        <polygon points={area} fill="url(#reqFill)" />
        <polyline points={coords} fill="none" stroke="#57b6c0" strokeWidth="2.5" strokeLinejoin="round" />
      </svg>
      {empty && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-slate-400">
          Нет заявок за период
        </div>
      )}
    </div>
  );
}

function activityMeta(action: string, result?: string | null, error?: string | null) {
  const a = (action || "").toUpperCase();
  const failed = !!(error || (result && /fail|error|deny|reject/i.test(result)));
  if (failed || a.includes("FAIL") || a.includes("AUTH_FAIL")) {
    return { label: "Ошибка аутентификации", tone: "bg-rose-50 text-danger", Icon: Icons.x };
  }
  if (a.includes("ORDER") || a.includes("CREATE")) {
    return { label: "Заявка создана", tone: "bg-sky-50 text-sky-600", Icon: Icons.plus };
  }
  if (a.includes("TRADE") || a.includes("FILL") || a.includes("EXEC")) {
    return { label: "Сделка получена", tone: "bg-sky-50 text-sky-600", Icon: Icons.trades };
  }
  if (a.includes("AUTH") || a.includes("LOGIN") || a.includes("TOKEN")) {
    return { label: "Успешная аутентификация", tone: "bg-emerald-50 text-emerald-600", Icon: Icons.check };
  }
  return { label: action || "Событие", tone: "bg-slate-100 text-slate-600", Icon: Icons.info };
}

function ActivityList({ items, empty }: { items: AuditRow[]; empty: string }) {
  if (!items.length) return <p className="text-sm text-slate-400">{empty}</p>;
  return (
    <ul className="space-y-3">
      {items.map((item) => {
        const meta = activityMeta(item.action, item.result, item.error);
        return (
          <li key={item.id} className="flex gap-2.5">
            <span className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full ${meta.tone}`}>
              <meta.Icon size={14} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">{meta.label}</div>
              <div className="text-[11px] text-slate-400">
                {formatTime(item.created_at)}
                {item.ip ? ` · ${item.ip}` : ""}
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

function BrokerActivity({
  st,
  range,
  onRange,
  errors,
  loading,
}: {
  st?: BrokerAPIStats;
  range: string;
  onRange: (v: string) => void;
  errors: ObsLog[];
  loading: boolean;
}) {
  if (loading && !st) return <div className="card text-slate-500">Загрузка…</div>;
  const today = st?.today;
  const week = st?.week;
  const month = st?.month;
  return (
    <div className="space-y-4">
      <section className="card space-y-4">
        <h2 className="font-semibold">Активность</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <SummaryChip label="Сегодня" value={formatNumber(today?.requests)} />
          <SummaryChip label="7 дней" value={formatNumber(week?.requests)} />
          <SummaryChip label="Месяц" value={formatNumber(month?.requests)} />
        </div>
        <div className="grid gap-3 sm:grid-cols-5">
          <SummaryChip label="Success rate" value={formatPct(today?.success_rate)} />
          <SummaryChip label="Errors" value={formatNumber(today?.errors)} />
          <SummaryChip label="Avg" value={formatMs(today?.avg_latency_ms)} />
          <SummaryChip label="P95" value={formatMs(today?.p95_latency_ms)} />
          <SummaryChip label="P99" value={formatMs(today?.p99_latency_ms)} />
        </div>
      </section>
      <section className="card space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">Запросы</h2>
          <select className="input w-auto" value={range} onChange={(e) => onRange(e.target.value)}>
            <option value="24h">24 часа</option>
            <option value="7d">7 дней</option>
            <option value="30d">30 дней</option>
          </select>
        </div>
        <MiniSeries series={st?.series || []} />
      </section>
      <section className="card space-y-3">
        <h2 className="font-semibold">Ошибки</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <SummaryChip label="Сегодня" value={formatNumber(today?.errors)} />
          <SummaryChip label="Неделя" value={formatNumber(week?.errors)} />
          <SummaryChip label="Месяц" value={formatNumber(month?.errors)} />
        </div>
        <div className="space-y-1 text-sm">
          {(st?.top_errors || []).map((e) => (
            <div key={e.error} className="flex justify-between rounded-lg bg-slate-50 px-3 py-2">
              <span className="font-mono text-xs">{e.error}</span>
              <span>{e.count}</span>
            </div>
          ))}
        </div>
        {errors.length === 0 ? (
          <p className="text-sm text-slate-400">Ошибок за выбранный период не обнаружено.</p>
        ) : (
          <ul className="space-y-2">
            {errors.map((e) => (
              <li key={e.id} className="rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-sm">
                <div className="font-medium text-danger">
                  {e.severity} · {e.error_code || e.subtype}
                </div>
                <div className="text-xs text-slate-500">{formatDateTime(e.timestamp)}</div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function MiniSeries({ series }: { series: Array<{ bucket: string; requests: number; successful: number; errors: number }> }) {
  const points = series.length ? series.map((s) => Number(s.requests) || 0) : [0];
  const err = series.length ? series.map((s) => Number(s.errors) || 0) : [0];
  const w = 480;
  const h = 120;
  const max = Math.max(...points, ...err, 1);
  const step = points.length > 1 ? w / (points.length - 1) : w;
  const line = (arr: number[]) => arr.map((p, i) => `${i * step},${h - (p / max) * (h - 16) - 8}`).join(" ");
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="h-32 w-full" preserveAspectRatio="none">
      <polyline points={line(points)} fill="none" stroke="#57b6c0" strokeWidth="2" />
      <polyline points={line(err)} fill="none" stroke="#d6455d" strokeWidth="2" />
    </svg>
  );
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4" onClick={onClose}>
      <div className="card max-h-[90vh] w-full max-w-lg overflow-y-auto" onClick={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between gap-3">
          <h3 className="text-lg font-semibold">{title}</h3>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Закрыть">
            <Icons.x size={16} />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
