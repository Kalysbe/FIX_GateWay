import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api, ApiError } from "../api";
import { useAdminStream } from "../hooks/useAdminStream";
import { liveRefetchInterval } from "../lib/poll";
import { groupDeals, tradeRows } from "../lib/trades";
import { formatDate, formatDateTime, sideLabel } from "../labels";
import { useBreadcrumbs } from "../components/breadcrumbs";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorBanner, PageSpinner } from "../components/feedback";
import type { AdminTrade } from "../types/admin";

export default function TradeDetailPage() {
  const { id } = useParams();
  const { connected } = useAdminStream();
  const query = useQuery({
    queryKey: ["admin-trade", id],
    queryFn: ({ signal }) => api<{ trades: AdminTrade[] }>(`/api/v1/admin/trades/${id}`, { signal }),
    enabled: !!id,
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
    refetchInterval: liveRefetchInterval(connected, 5000),
  });
  const deal = groupDeals(query.data?.trades ?? [])[0];
  useBreadcrumbs([{ label: "Сделки", to: "/trades" }, { label: deal?.label || "Карточка сделки" }]);

  const back = <Link to="/trades" className="btn-muted">К списку сделок</Link>;
  if (query.isLoading) return <PageSpinner label="Загрузка сделки…" />;
  if (query.error) return <div className="space-y-4">
    <PageHeader title="Карточка сделки" actions={back} />
    {query.error instanceof ApiError && query.error.status === 404
      ? <EmptyState title="Сделка не найдена" hint="Проверьте адрес или вернитесь к списку сделок." />
      : <ErrorBanner error={query.error} onRetry={() => query.refetch()} />}
  </div>;
  if (!deal) return <div className="space-y-4"><PageHeader title="Карточка сделки" actions={back} /><EmptyState title="Нет данных сделки" /></div>;

  const executionDate = deal.trades[0].execution_date;
  return <article className="trade-detail-page space-y-4" aria-label={`Карточка сделки ${deal.label}`}>
    <PageHeader
      title={`Сделка ${deal.label}`}
      subtitle="FIX Gateway · Зарегистрированная сделка QUIK"
      actions={<div className="trade-detail-actions flex flex-wrap gap-2">
        {back}
        <button type="button" className="btn" onClick={() => window.print()} disabled={query.isFetching}>Печать</button>
      </div>}
    />
    <section className="card" aria-label="Общие сведения о сделке">
      <dl className="trade-overview-fields">
        <Field label="Инструмент">{deal.symbol || "—"}</Field>
        <Field label="Торговый класс">{deal.board || "—"}</Field>
        <Field label="Дата сделки">{executionDate ? formatDate(executionDate + "T00:00:00") : "—"}</Field>
        <Field label="Стороны">{deal.paired ? "Покупка и продажа сохранены" : "Требуется восстановление отчёта второй стороны"}</Field>
      </dl>
    </section>
    {!deal.paired && <p className="trade-pair-notice">В БД пока нет записи одной из сторон. Требуется сверка с историей QUIK.</p>}
    <div className="trade-leg-grid">
      {tradeRows(deal).map(trade => <section key={trade.id} className="trade-leg-card card" data-source={trade.source} aria-label={`${sideLabel(trade.side)} · ${trade.exec_id || "Контрагент"}`}>
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="font-semibold">{sideLabel(trade.side)}</h2>
          <span className={trade.side === "BUY" ? "badge-success" : "badge-danger"}>{trade.has_fix_report ? (trade.has_quik_record ? "FIX · История QUIK" : "Отчёт FIX") : "История QUIK"}</span>
        </div>
        <dl className="trade-leg-fields">
          <Field label="Номер сделки QUIK">{trade.quik_trade_num || deal.label}</Field>
          {trade.trade_match_id && <Field label="Полный идентификатор сделки">{trade.trade_match_id}</Field>}
          <Field label="ID стороны в БД">{trade.id}</Field>
          {trade.exec_id && <Field label="ExecID">{trade.exec_id}</Field>}
          <Field label="Фирма">{trade.firm_name || "—"}</Field>
          <Field label="Код фирмы">{trade.quik_firm || "—"}</Field>
          <Field label="Код клиента">{trade.client_code || "—"}</Field>
          <Field label="Торговый счёт">{trade.account || "—"}</Field>
          <Field label="Время сделки">{trade.trade_time_known === false ? "Не передано в истории QUIK" : formatDateTime(trade.created_at)}</Field>
          <Field label="Номер заявки QUIK">{trade.quik_order_num || trade.quik_order_id || "—"}</Field>
          <Field label="Количество сделки">{trade.last_qty}</Field>
          <Field label="Цена сделки">{trade.last_px}</Field>
          {trade.cum_qty != null && <Field label="Исполнено по заявке">{trade.cum_qty}</Field>}
          {trade.leaves_qty != null && <Field label="Остаток заявки">{trade.leaves_qty}</Field>}
          {trade.order_id && <><Field label="ID заявки клиента">{trade.client_order_id || "—"}</Field>
          <Field label="ID заявки Gateway"><Link className="text-accent hover:underline" to={`/orders/${trade.order_id}`}>{trade.order_id}</Link></Field></>}
          {trade.fix_cl_ord_id && <Field label="FIX ClOrdID">{trade.fix_cl_ord_id}</Field>}
          {trade.quik_order_id && <Field label="QUIK OrderID">{trade.quik_order_id}</Field>}
          {trade.adapter_id && <Field label="Адаптер">{trade.adapter_id}</Field>}
        </dl>
      </section>)}
    </div>
    <p className="trade-document-note">Время сделки указано в часовом поясе {Intl.DateTimeFormat().resolvedOptions().timeZone}.</p>
  </article>;
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div><dt>{label}</dt><dd>{children}</dd></div>;
}
