import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useParams } from "react-router-dom";
import { clearToken, isSessionValid, watchSessionExpiry } from "./api";
import AppShell from "./components/AppShell";
import { BreadcrumbProvider } from "./components/breadcrumbs";
import LoginPage from "./pages/LoginPage";
import DashboardPage from "./pages/DashboardPage";
import BrokersPage from "./pages/BrokersPage";
import BrokerDetailPage from "./pages/BrokerDetailPage";
import TokensPage from "./pages/TokensPage";
import OrdersPage from "./pages/OrdersPage";
import OrderDetailPage from "./pages/OrderDetailPage";
import TradesPage from "./pages/TradesPage";
import MarketDataPage from "./pages/MarketDataPage";
import PositionsPage from "./pages/PositionsPage";
import AdaptersPage from "./pages/AdaptersPage";
import ReferencePage from "./pages/ReferencePage";
import LogsPage from "./pages/LogsPage";
import NotificationsPage from "./pages/NotificationsPage";

function Private() {
  const valid = isSessionValid();
  useEffect(() => {
    if (!valid) {
      clearToken();
      return;
    }
    return watchSessionExpiry();
  }, [valid]);
  if (!valid) return <Navigate to="/login" replace />;
  return (
    <BreadcrumbProvider>
      <AppShell>
        <Outlet />
      </AppShell>
    </BreadcrumbProvider>
  );
}

function BrokerTokensRedirect() {
  const { id } = useParams();
  return <Navigate to={`/brokers/${id}?tab=tokens`} replace />;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<Private />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/brokers" element={<BrokersPage />} />
        <Route path="/brokers/:id" element={<BrokerDetailPage />} />
        <Route path="/brokers/:id/tokens" element={<BrokerTokensRedirect />} />
        <Route path="/tokens" element={<TokensPage />} />
        <Route path="/orders" element={<OrdersPage />} />
        <Route path="/orders/:id" element={<OrderDetailPage />} />
        <Route path="/trades" element={<TradesPage />} />
        <Route path="/market-data" element={<MarketDataPage />} />
        <Route path="/positions" element={<PositionsPage />} />
        <Route path="/adapters" element={<AdaptersPage />} />
        <Route path="/reference" element={<ReferencePage />} />
        <Route path="/logs" element={<LogsPage />} />
        <Route path="/logs/:kind" element={<LogsPage />} />
        <Route path="/notifications" element={<NotificationsPage />} />
      </Route>
    </Routes>
  );
}
