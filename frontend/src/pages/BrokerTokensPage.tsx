import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { formatDate, formatDateTime, statusLabel } from "../labels";

const ALL_PERMS = [
  "orders:create",
  "orders:read",
  "orders:cancel",
  "orders:replace",
  "trades:read",
  "positions:read",
] as const;

const READ_ONLY = ["orders:read", "trades:read", "positions:read"];

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

export default function BrokerTokensPage() {
  const { id } = useParams();
  const qc = useQueryClient();
  const broker = useQuery({ queryKey: ["broker", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}`) });
  const tokens = useQuery({ queryKey: ["tokens", id], queryFn: () => api<{ tokens: Token[] }>(`/api/v1/admin/brokers/${id}/tokens`) });
  const security = useQuery({ queryKey: ["security", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/security`) });
  const whitelist = useQuery({ queryKey: ["ipwl", id], queryFn: () => api<any>(`/api/v1/admin/brokers/${id}/ip-whitelist`) });

  const [showCreate, setShowCreate] = useState(false);
  const [tokenName, setTokenName] = useState("Production API");
  const [quikFirm, setQuikFirm] = useState("");
  const [expiresIn, setExpiresIn] = useState(180);
  const [perms, setPerms] = useState<string[]>([...ALL_PERMS]);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealTitle, setRevealTitle] = useState("ТОКЕН СОЗДАН");
  const [revealNote, setRevealNote] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<Token | null>(null);
  const [editFirm, setEditFirm] = useState<Token | null>(null);
  const [editFirmValue, setEditFirmValue] = useState("");
  const [newIP, setNewIP] = useState("");

  const b = broker.data?.broker;
  const st = security.data?.security;

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
      body: JSON.stringify({ ip: newIP, note: "" }),
    });
    setNewIP("");
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

  const statusClass = useMemo(
    () =>
      ({
        ACTIVE: "text-accent",
        REVOKED: "text-danger",
        EXPIRED: "text-warn",
        DISABLED: "text-slate-500",
      }) as Record<string, string>,
    [],
  );

  if (broker.isLoading) return <div>Загрузка…</div>;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <Link to={`/brokers/${id}`} className="text-sm text-slate-500 hover:text-accent">
            ← {b?.name || "Брокер"}
          </Link>
          <h1 className="text-2xl font-semibold">API-токены</h1>
          <p className="font-mono text-sm text-slate-500">{b?.code} · управление учётными данными</p>
        </div>
        <button className="btn" onClick={openCreate}>
          Создать токен
        </button>
      </div>

      <div className="grid gap-3 md:grid-cols-3 lg:grid-cols-6">
        {[
          ["Активные", st?.active_tokens],
          ["Истёкшие", st?.expired_tokens],
          ["Отозванные", st?.revoked_tokens],
          ["Неудачные входы 24ч", st?.failed_auth_attempts_24h],
          ["Последний IP", st?.last_used_ip || "—"],
          ["Последняя активность", st?.last_api_activity ? formatDateTime(st.last_api_activity) : "—"],
        ].map(([label, value]) => (
          <div key={String(label)} className="card py-3">
            <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
            <div className="mt-1 truncate text-lg font-semibold">{value ?? "—"}</div>
          </div>
        ))}
      </div>

      <div className="card space-y-3 overflow-x-auto">
        <h2 className="font-medium">Токены</h2>
        <table className="table">
          <thead>
            <tr>
              <th>Название</th>
              <th>Статус</th>
              <th>Фирма (5018)</th>
              <th>Создан</th>
              <th>Истекает</th>
              <th>Последнее использование</th>
              <th>Последний IP</th>
              <th>Права</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(tokens.data?.tokens || []).map((t) => (
              <tr key={t.id}>
                <td>
                  <div className="font-medium">{t.name}</div>
                  <div className="font-mono text-xs text-slate-500">{t.token_prefix}…</div>
                </td>
                <td className={statusClass[t.status] || ""}>{statusLabel(t.status)}</td>
                <td className="font-mono">{t.quik_firm || <span className="text-danger">—</span>}</td>
                <td>{formatDateTime(t.created_at)}</td>
                <td>{t.expires_at ? formatDate(t.expires_at) : "Без срока"}</td>
                <td>{t.last_used_at ? formatDateTime(t.last_used_at) : "—"}</td>
                <td className="font-mono text-xs">{t.last_used_ip || "—"}</td>
                <td className="max-w-xs text-xs text-slate-600">{(t.permissions || []).join(", ")}</td>
                <td className="space-x-2 whitespace-nowrap">
                  {t.status === "ACTIVE" && (
                    <>
                      <button
                        className="btn-muted"
                        onClick={() => {
                          setEditFirm(t);
                          setEditFirmValue(t.quik_firm || "");
                        }}
                      >
                        Фирма
                      </button>
                      <button className="btn-muted" onClick={() => rotate(t.id)}>
                        Ротация
                      </button>
                      <button className="btn-muted" onClick={() => setConfirmRevoke(t)}>
                        Отозвать
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="font-medium">IP-whitelist</h2>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={!!(whitelist.data?.ip_whitelist_enabled ?? b?.ip_whitelist_enabled)}
              onChange={(e) => toggleWhitelist(e.target.checked)}
            />
            Включить whitelist
          </label>
        </div>
        <p className="text-sm text-slate-500">
          При включении API-токены этого брокера доступны только с указанных IP/CIDR.
        </p>
        <form onSubmit={addIP} className="flex gap-2">
          <input
            className="input max-w-sm"
            placeholder="185.10.10.10 или 185.10.10.0/24"
            value={newIP}
            onChange={(e) => setNewIP(e.target.value)}
            required
          />
          <button className="btn" type="submit">
            Добавить IP
          </button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>IP / CIDR</th>
              <th>Заметка</th>
              <th>Добавлен</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(whitelist.data?.entries || []).map((e: any) => (
              <tr key={e.id}>
                <td className="font-mono">{e.ip_cidr}</td>
                <td>{e.note || "—"}</td>
                <td>{formatDateTime(e.created_at)}</td>
                <td>
                  <button className="btn-muted" onClick={() => removeIP(e.id)}>
                    Удалить
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="card w-full max-w-lg space-y-4">
            <h3 className="text-lg font-semibold">Создать API-токен</h3>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Название токена</span>
              <input className="input" value={tokenName} onChange={(e) => setTokenName(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Код фирмы (FIX 5018)</span>
              <input
                className="input font-mono"
                placeholder="например 2040"
                value={quikFirm}
                onChange={(e) => setQuikFirm(e.target.value)}
                required
              />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="text-slate-500">Срок действия</span>
              <select className="input" value={expiresIn} onChange={(e) => setExpiresIn(Number(e.target.value))}>
                <option value={30}>30 дней</option>
                <option value={90}>90 дней</option>
                <option value={180}>180 дней</option>
                <option value={365}>365 дней</option>
                <option value={0}>Без срока</option>
              </select>
            </label>
            <div className="space-y-2">
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
              <button className="btn-muted" onClick={() => setShowCreate(false)}>
                Отмена
              </button>
              <button
                className="btn"
                disabled={createToken.isPending || !tokenName || !quikFirm.trim() || perms.length === 0}
                onClick={() => createToken.mutate()}
              >
                Создать
              </button>
            </div>
          </div>
        </div>
      )}

      {editFirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="card w-full max-w-md space-y-4">
            <h3 className="text-lg font-semibold">Код фирмы (FIX 5018)</h3>
            <p className="text-sm text-slate-600">
              Токен <span className="font-medium">{editFirm.name}</span>. Значение уходит в NewOrderSingle tag 5018.
            </p>
            <input
              className="input font-mono"
              value={editFirmValue}
              onChange={(e) => setEditFirmValue(e.target.value)}
              placeholder="код фирмы"
            />
            <div className="flex justify-end gap-2">
              <button className="btn-muted" onClick={() => setEditFirm(null)}>
                Отмена
              </button>
              <button className="btn" disabled={!editFirmValue.trim()} onClick={() => saveFirm()}>
                Сохранить
              </button>
            </div>
          </div>
        </div>
      )}

      {revealedToken && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4">
          <div className="card w-full max-w-xl space-y-4 border-warn/40">
            <div className="text-center">
              <div className="text-xs uppercase tracking-[0.2em] text-warn">{revealTitle}</div>
              <p className="mt-2 text-sm text-slate-600">{revealNote}</p>
            </div>
            <div className="break-all rounded-lg border border-slate-200 bg-surface p-4 font-mono text-sm">
              {revealedToken}
            </div>
            <div className="flex justify-end gap-2">
              <button className="btn" onClick={() => navigator.clipboard.writeText(revealedToken)}>
                Копировать токен
              </button>
              <button className="btn-muted" onClick={() => setRevealedToken(null)}>
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmRevoke && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4">
          <div className="card w-full max-w-md space-y-4">
            <h3 className="text-lg font-semibold">Отозвать токен?</h3>
            <p className="text-sm text-slate-600">
              <span className="font-medium">{confirmRevoke.name}</span> перестанет работать сразу.
              Любой запрос с этим токеном получит HTTP 401.
            </p>
            <div className="flex justify-end gap-2">
              <button className="btn-muted" onClick={() => setConfirmRevoke(null)}>
                Отмена
              </button>
              <button className="btn" onClick={() => revoke(confirmRevoke.id)}>
                Отозвать
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
