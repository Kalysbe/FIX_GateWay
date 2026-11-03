import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { apiWithHeaders } from "../api";
import { dbValue, parseDBJSON, type DBOrder } from "../lib/rawOrders";
import { rawColumns, rawSearchText } from "../components/rawColumns";
import { quikColumns, type QuikColumn } from "../components/quikColumns";
import { DataTable } from "../components/DataTable";
import { ErrorBanner } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { instrumentPath } from "../lib/quotes";

// The terms of a bond; option, scale and the other Securities columns are in
// the column menu.
const SECURITY_COLUMNS: QuikColumn[] = [
  { id: "instrument", header: "Инструмент", field: "SecCode", sub: "SecShortName", link: r => instrumentPath(r.ClassCode, r.SecCode) },
  { id: "class", header: "Класс", field: "ClassCode" },
  { id: "isin", header: "ISIN", field: "ISINCode" },
  { id: "regnumber", header: "Рег. номер", field: "RegNumber" },
  { id: "facevalue", header: "Номинал", field: "FaceValue", kind: "number", frac: 2, sub: "FaceUnit" },
  { id: "coupon", header: "Купон", field: "CouponValue", kind: "number", frac: 2 },
  { id: "nextcoupon", header: "След. купон", field: "NextCoupon", kind: "date" },
  { id: "couponperiod", header: "Период, дн.", field: "CouponPeriod", kind: "number" },
  { id: "matdate", header: "Погашение", field: "MatDate", kind: "date" },
  { id: "days", header: "Дней до погаш.", field: "DaysToMatDate", kind: "number" },
  { id: "minstep", header: "Шаг цены", field: "MinStep", kind: "number" },
  { id: "lotsize", header: "Лот", field: "LotSize", kind: "number" },
];

export default function ReferencePage() {
  const [tab, setTab] = useState<"instruments" | "firms">("instruments");

  const instruments = useQuery({
    queryKey: ["admin-instruments"],
    // Current rows of QUIK "Securities" of the served classes, as stored.
    queryFn: async ({ signal }) => (await apiWithHeaders<DBOrder[]>("/api/v1/admin/instruments?limit=1000", { signal }, parseDBJSON)).data ?? [],
  });
  const firms = useQuery({
    queryKey: ["admin-firms"],
    // Rows of QUIK "Firms" as stored.
    queryFn: async ({ signal }) => (await apiWithHeaders<DBOrder[]>("/api/v1/admin/firms", { signal }, parseDBJSON)).data ?? [],
  });

  const instrumentRows = instruments.data ?? [];
  const firmRows = firms.data ?? [];

  return (
    <div className="raw-orders space-y-6">
      <PageHeader title="Справочник" subtitle="Инструменты и фирмы из базы QUIK" />

      <div className="flex flex-wrap items-center gap-2">
        <button
          className={tab === "instruments" ? "btn" : "btn-muted"}
          onClick={() => setTab("instruments")}
        >
          Инструменты ({instruments.data ? instrumentRows.length : "…"})
        </button>
        <button className={tab === "firms" ? "btn" : "btn-muted"} onClick={() => setTab("firms")}>
          Фирмы ({firms.data ? firmRows.length : "…"})
        </button>
      </div>

      {tab === "instruments" ? <>
        <p className="text-xs text-muted">Таблица QUIK Securities за последнюю дату. Остальные поля — в меню столбцов</p>
        {instruments.error && <ErrorBanner error={instruments.error} onRetry={() => instruments.refetch()} />}
        <DataTable title="Инструменты" rows={instrumentRows} columns={quikColumns(instrumentRows, SECURITY_COLUMNS)} rowKey={i => `${dbValue(i.ClassCode)}\u0000${dbValue(i.SecCode)}`} getSearchText={rawSearchText(instrumentRows)} loading={instruments.isLoading} exportName="instruments" showTotals={false} emptyTitle={instruments.error ? "Таблица Securities недоступна" : "В Securities нет строк"} />
      </> : <>
        <p className="text-xs text-muted">Строки таблицы QUIK Firms в том виде, в каком они хранятся в базе</p>
        {firms.error && <ErrorBanner error={firms.error} onRetry={() => firms.refetch()} />}
        <DataTable title="Фирмы" rows={firmRows} columns={rawColumns(firmRows)} rowKey={f => `${dbValue(f.FirmId)}\u0000${dbValue(f.Exchange)}`} getSearchText={rawSearchText(firmRows)} loading={firms.isLoading} exportName="firms" showTotals={false} emptyTitle={firms.error ? "Таблица Firms недоступна" : "В Firms нет строк"} />
      </>}
    </div>
  );
}
