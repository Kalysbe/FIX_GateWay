import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { clearToken, getToken } from "./api";
import LoginPage from "./pages/LoginPage";
import DashboardPage from "./pages/DashboardPage";
import BrokersPage from "./pages/BrokersPage";
import BrokerDetailPage from "./pages/BrokerDetailPage";
import BrokerTokensPage from "./pages/BrokerTokensPage";
import OrdersPage from "./pages/OrdersPage";
import OrderDetailPage from "./pages/OrderDetailPage";
import TradesPage from "./pages/TradesPage";
import AdaptersPage from "./pages/AdaptersPage";
import ReferencePage from "./pages/ReferencePage";

function Shell({ children }: { children: React.ReactNode }) {
  const loc = useLocation();
  const nav = [
    ["/", "Дашборд"],
    ["/brokers", "Брокеры"],
    ["/orders", "Ордера"],
    ["/trades", "Сделки"],
    ["/adapters", "Адаптеры"],
    ["/reference", "Справочник"],
  ] as const;
  return (
    <div className="min-h-screen bg-[#f4f7f8] text-[#1a2332]">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-4 py-3">
          <div>
            <div className="font-display text-lg font-bold tracking-wide text-accent">FIX Gateway</div>
            <div className="text-xs text-slate-500">Маршрутизация ордеров брокера · QUIK</div>
          </div>
          <nav className="flex items-center gap-2">
            {nav.map(([to, label]) => {
              const active = loc.pathname === to || (to !== "/" && loc.pathname.startsWith(to));
              return (
                <Link
                  key={to}
                  to={to}
                  className={`rounded-lg px-3 py-1.5 text-sm ${
                    active
                      ? "bg-accent text-white"
                      : "text-slate-600 hover:bg-slate-100 hover:text-ink"
                  }`}
                >
                  {label}
                </Link>
              );
            })}
            <button
              className="btn-muted ml-2 border-accent text-accent"
              onClick={() => {
                clearToken();
                window.location.href = "/login";
              }}
            >
              Выйти
            </button>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-4 py-6">{children}</main>
    </div>
  );
}

function Private({ children }: { children: React.ReactNode }) {
  if (!getToken()) return <Navigate to="/login" replace />;
  return <Shell>{children}</Shell>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={<Private><DashboardPage /></Private>} />
      <Route path="/brokers" element={<Private><BrokersPage /></Private>} />
      <Route path="/brokers/:id" element={<Private><BrokerDetailPage /></Private>} />
      <Route path="/brokers/:id/tokens" element={<Private><BrokerTokensPage /></Private>} />
      <Route path="/orders" element={<Private><OrdersPage /></Private>} />
      <Route path="/orders/:id" element={<Private><OrderDetailPage /></Private>} />
      <Route path="/trades" element={<Private><TradesPage /></Private>} />
      <Route path="/adapters" element={<Private><AdaptersPage /></Private>} />
      <Route path="/reference" element={<Private><ReferencePage /></Private>} />
    </Routes>
  );
}
