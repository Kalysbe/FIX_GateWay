import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useParams } from "react-router-dom";
import { clearToken, isSessionValid, watchSessionExpiry } from "./api";
import AppShell from "./components/AppShell";
import { BreadcrumbProvider } from "./components/breadcrumbs";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { ToastProvider } from "./components/Toast";
import { AdminStreamProvider } from "./hooks/useAdminStream";
import LoginPage from "./pages/LoginPage";
import DashboardPage from "./pages/DashboardPage";
import MonitoringPage from "./pages/MonitoringPage";
import BrokersPage from "./pages/BrokersPage";
import BrokerDetailPage from "./pages/BrokerDetailPage";
import TokensPage from "./pages/TokensPage";
import OrdersPage from "./pages/OrdersPage";
import OrderDetailPage from "./pages/OrderDetailPage";
import TradesPage from "./pages/TradesPage";
import MarketDataPage from "./pages/MarketDataPage";
import QuotesPage from "./pages/QuotesPage";
import PositionsPage from "./pages/PositionsPage";
import AdaptersPage from "./pages/AdaptersPage";
import ReferencePage from "./pages/ReferencePage";
import LogsPage from "./pages/LogsPage";
import NotificationsPage from "./pages/NotificationsPage";
import RateLimitsPage from "./pages/RateLimitsPage";

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
    <ToastProvider>
      <AdminStreamProvider>
        <BreadcrumbProvider>
          <AppShell>
            <ErrorBoundary>
              <Outlet />
            </ErrorBoundary>
          </AppShell>
        </BreadcrumbProvider>
      </AdminStreamProvider>
    </ToastProvider>
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
        <Route path="/monitoring" element={<MonitoringPage />} />
        <Route path="/rate-limits" element={<RateLimitsPage />} />
        <Route path="/brokers" element={<BrokersPage />} />
        <Route path="/brokers/:id" element={<BrokerDetailPage />} />
        <Route path="/brokers/:id/tokens" element={<BrokerTokensRedirect />} />
        <Route path="/tokens" element={<TokensPage />} />
        <Route path="/orders" element={<OrdersPage />} />
        <Route path="/orders/:id" element={<OrderDetailPage />} />
        <Route path="/trades" element={<TradesPage />} />
        <Route path="/market-data" element={<MarketDataPage />} />
        <Route path="/quotes" element={<QuotesPage />} />
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
