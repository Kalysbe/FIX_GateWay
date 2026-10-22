import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api";
import { formatDateTime } from "../labels";
import type { APIToken, TokenOverview } from "../types/admin";
import { Icons } from "./icons";
import { StatusBadge } from "./StatusBadge";
import { ErrorBanner, PageSpinner } from "./feedback";

const permissionLabels: Record<string, string> = {
  "orders:create": "Создание заявок", "orders:read": "Просмотр заявок и рынка",
  "orders:cancel": "Снятие заявок", "orders:replace": "Изменение заявок",
  "trades:read": "Просмотр сделок", "positions:read": "Просмотр позиций", "positions:write": "Изменение позиций",
};

function Field({ label, children, mono = false }: { label: string; children: ReactNode; mono?: boolean }) {
  return <div className="min-w-0"><dt className="text-[11px] text-muted">{label}</dt><dd className={`mt-1 break-words text-[13px] ${mono ? "break-all font-mono text-[11px]" : ""}`}>{children || "—"}</dd></div>;
}

export function TokenLookup({ tokenId, onSelect }: { tokenId: string | null; onSelect: (id: string | null) => void }) {
  const [input, setInput] = useState("");
  const [visible, setVisible] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [matches, setMatches] = useState<APIToken[] | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => { active.current?.abort(); active.current = null; }, []);
  const detail = useQuery({
    queryKey: ["admin-token-overview", tokenId],
    queryFn: ({ signal }) => api<TokenOverview>(`/api/v1/admin/tokens/${tokenId}`, { signal, cache: "no-store" }),
    enabled: !!tokenId, retry: false,
  });

  function clear() {
    active.current?.abort(); active.current = null;
    setLoading(false); setInput(""); setVisible(false); setError(""); setMatches(null); setHasMore(false); onSelect(null);
  }

  async function search(event: FormEvent) {
    event.preventDefault();
    if (active.current || !input.trim()) return;
    const token = input.trim();
    const controller = new AbortController();
    active.current = controller;
    setLoading(true); setError(""); setMatches(null); setHasMore(false); onSelect(null);
    // Keep the credential out of URLs, query keys and mutation caches.
    setInput(""); setVisible(false);
    try {
      const result = await api<{ tokens: APIToken[]; has_more: boolean }>("/api/v1/admin/tokens/search", {
        method: "POST", body: JSON.stringify({ token }), signal: controller.signal, cache: "no-store",
      });
      if (active.current !== controller) return;
      setMatches(result.tokens); setHasMore(result.has_more);
      if (result.tokens.length === 1) onSelect(result.tokens[0].id);
    } catch (err) {
      if (active.current !== controller || controller.signal.aborted) return;
      setError(err instanceof ApiError && err.status === 400 ? "Введите полный API-токен, его ID или префикс из 8 символов." : "Не удалось выполнить поиск. Повторите попытку, введя токен заново.");
    } finally {
      if (active.current === controller) { active.current = null; setLoading(false); }
    }
  }

  const overview = tokenId ? detail.data : undefined;
  const token = overview?.token;
  return <section id="token-lookup" className="card space-y-5" aria-labelledby="token-lookup-title">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h2 id="token-lookup-title" className="text-base font-semibold">Найти токен</h2><p id="token-lookup-hint" className="mt-1 text-xs leading-relaxed text-muted">Полный API-токен, его ID или префикс из 8 символов.</p></div>
      {(tokenId || matches !== null || loading || error) && <button type="button" className="btn-muted" onClick={clear}>{loading ? "Отменить" : "Очистить"}</button>}
    </div>
    <form method="post" onSubmit={search} className="flex flex-col gap-3 sm:flex-row" aria-busy={loading}>
      <div className="relative min-w-0 flex-1">
        <label className="sr-only" htmlFor="token-lookup-input">Токен, ID или префикс</label>
        <input id="token-lookup-input" name="token-lookup" className="input min-h-11 pr-12 font-mono text-base sm:text-sm" type={visible ? "text" : "password"}
          autoComplete="off" autoCapitalize="none" spellCheck={false} placeholder="Вставьте токен, ID или префикс" aria-describedby="token-lookup-hint"
          minLength={8} maxLength={512} value={input} onChange={e => setInput(e.target.value)} disabled={loading} required />
        <button type="button" className="absolute right-0 top-0 grid h-11 w-11 place-items-center rounded-md text-muted hover:text-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
          aria-label={visible ? "Скрыть поисковый запрос" : "Показать поисковый запрос"} aria-controls="token-lookup-input" onClick={() => setVisible(v => !v)}><Icons.eye size={18} /></button>
      </div>
      <button type="submit" className="btn min-h-11 justify-center gap-2 sm:min-w-32" disabled={loading || !input.trim()}><Icons.search size={16} />{loading ? "Поиск…" : "Найти"}</button>
    </form>
    {loading && <p role="status" className="text-sm text-muted">Ищем токен…</p>}
    {!tokenId && error && <p role="alert" className="rounded-lg border border-rose-100 bg-rose-50 p-3 text-sm text-danger">{error}</p>}
    {!tokenId && matches?.length === 0 && <div role="status" className="rounded-lg bg-surface px-4 py-5"><p className="text-sm font-medium">Токен не найден</p><p className="mt-1 text-xs text-muted">Проверьте ID или скопируйте токен полностью. По префиксу используются все 8 символов.</p></div>}
    {matches?.length === 1 && tokenId === matches[0].id && <p role="status" className="text-xs text-muted">Токен найден. Общая информация ниже.</p>}
    {matches && matches.length > 1 && <div className="space-y-3">
      <p role="status" className="text-xs text-muted">{hasMore ? "Показаны первые 20 совпадений. Уточните поиск полным токеном или ID." : `Найдено токенов: ${matches.length}. Выберите нужный.`}</p>
      <ul className="divide-y divide-line rounded-lg border border-line">{matches.map(match => <li key={match.id}><button type="button" className={`flex min-h-14 w-full flex-wrap items-center justify-between gap-2 px-4 py-3 text-left hover:bg-surface ${tokenId === match.id ? "bg-surface" : ""}`} onClick={() => onSelect(match.id)}>
        <span className="min-w-0"><span className="block break-words text-sm font-medium">{match.name}</span><span className="text-xs text-muted">{match.broker_name || match.broker_code} · <span className="font-mono">{match.token_prefix}…</span></span></span><StatusBadge status={match.status} />
      </button></li>)}</ul>
    </div>}
    {tokenId && detail.isLoading && <PageSpinner label="Загружаем сведения о токене…" />}
    {tokenId && detail.error && <ErrorBanner error={detail.error} onRetry={() => void detail.refetch()} />}
    {overview && token && <div role="region" className="space-y-5 border-t border-line pt-5" aria-labelledby="token-overview-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0"><div className="flex flex-wrap items-center gap-3"><h3 id="token-overview-title" className="min-w-0 max-w-full break-words text-lg font-semibold">{token.name}</h3><StatusBadge status={token.status} /></div><p className="mt-1 font-mono text-xs text-muted">{token.token_prefix}…</p>{token.description && <p className="mt-2 break-words text-sm text-muted">{token.description}</p>}</div>
        <div className="flex flex-wrap gap-2"><button type="button" className="btn-muted" onClick={() => void detail.refetch()} disabled={detail.isFetching}>Обновить</button><Link className="btn-muted" to={`/brokers/${overview.broker.id}?tab=tokens`}>Управление токеном<Icons.chevron size={13} /></Link></div>
      </div>
      {overview.broker.status !== "ACTIVE" && <p role="status" className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">Брокер отключён или заблокирован. Доступ по его токенам ограничен независимо от статуса токена.</p>}
      <div className="grid gap-6 lg:grid-cols-3">
        <div className="min-w-0"><h4 className="mb-4 text-xs font-semibold uppercase tracking-wide text-muted">Общая информация</h4><dl className="grid gap-4">
          <Field label="ID токена" mono>{token.id}</Field><Field label="Создан">{formatDateTime(token.created_at)}</Field>
          <Field label="Срок действия">{token.expires_at ? formatDateTime(token.expires_at) : "Без ограничения срока"}</Field>
          {token.revoked_at && <Field label="Отозван">{formatDateTime(token.revoked_at)}</Field>}
          <Field label="Последнее использование">{token.last_used_at ? formatDateTime(token.last_used_at) : "Нет данных об использовании"}</Field><Field label="Последний IP" mono>{token.last_used_ip}</Field>
          {token.rotated_from && <Field label="Выпущен взамен токена" mono>{token.rotated_from}</Field>}
        </dl></div>
        <div className="min-w-0"><h4 className="mb-4 text-xs font-semibold uppercase tracking-wide text-muted">Права доступа</h4><dl className="mb-4"><Field label="Фирма QUIK" mono>{token.quik_firm}</Field></dl>
          {token.permissions.length ? <ul className="space-y-2">{token.permissions.map(permission => <li key={permission} className="flex items-center gap-2 text-xs" title={permission}><Icons.check size={14} className="shrink-0 text-accent" /><span>{permissionLabels[permission] || permission}</span></li>)}</ul> : <p className="text-xs text-muted">Права не назначены</p>}
        </div>
        <div className="min-w-0"><h4 className="mb-4 text-xs font-semibold uppercase tracking-wide text-muted">Брокер и ограничения</h4><dl className="grid gap-4">
          <Field label="Брокер"><Link className="font-medium hover:text-accent" to={`/brokers/${overview.broker.id}`}>{overview.broker.name}</Link><span className="ml-2 font-mono text-[11px] text-muted">{overview.broker.code}</span></Field>
          <Field label="Статус брокера"><StatusBadge status={overview.broker.status} /></Field>
          <Field label="Лимиты брокера">{overview.broker.rate_limit_rps} запросов/с · {overview.broker.rate_limit_orders_per_sec} заявок/с</Field>
          <Field label="Доступ по IP">{overview.broker.ip_whitelist_enabled ? "Только разрешённые адреса" : "IP-ограничения отключены"}</Field>
        </dl>
          {overview.broker.ip_whitelist_enabled && (overview.ip_whitelist.length ? <ul className="mt-3 space-y-2">{overview.ip_whitelist.map(entry => <li key={entry.id} className="break-words text-xs"><span className="font-mono">{entry.ip_cidr}</span>{entry.note && <span className="ml-2 text-muted">{entry.note}</span>}</li>)}</ul> : <p className="mt-3 text-xs text-danger">Нет разрешённых IP-адресов: доступ закрыт.</p>)}
        </div>
      </div>
    </div>}
  </section>;
}
