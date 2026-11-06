import { Suspense, useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useParams } from "react-router-dom";
import { clearToken, isSessionValid, watchSessionExpiry } from "./api";
import AppShell from "./components/AppShell";
import { BreadcrumbProvider } from "./components/breadcrumbs";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { ToastProvider } from "./components/Toast";
import { ConfirmProvider } from "./components/confirm";
import { AdminStreamProvider } from "./hooks/useAdminStream";
import { PageSpinner } from "./components/feedback";
import { lazyPage } from "./lib/lazyPage";

// Each page is its own chunk: the login screen and the shell load without the
// code of all the pages behind them.
const LoginPage = lazyPage(() => import("./pages/LoginPage"));
const DashboardPage = lazyPage(() => import("./pages/DashboardPage"));
const MonitoringPage = lazyPage(() => import("./pages/MonitoringPage"));
const GrpcPage = lazyPage(() => import("./pages/GrpcPage"));
const BrokersPage = lazyPage(() => import("./pages/BrokersPage"));
const BrokerDetailPage = lazyPage(() => import("./pages/BrokerDetailPage"));
const TokensPage = lazyPage(() => import("./pages/TokensPage"));
const OrdersPage = lazyPage(() => import("./pages/OrdersPage"));
const OrderDetailPage = lazyPage(() => import("./pages/OrderDetailPage"));
const TradesPage = lazyPage(() => import("./pages/TradesPage"));
const MarketDataPage = lazyPage(() => import("./pages/MarketDataPage"));
const MarketInstrumentPage = lazyPage(() => import("./pages/MarketInstrumentPage"));
const QuotesPage = lazyPage(() => import("./pages/QuotesPage"));
const PositionsPage = lazyPage(() => import("./pages/PositionsPage"));
const AdaptersPage = lazyPage(() => import("./pages/AdaptersPage"));
const ReferencePage = lazyPage(() => import("./pages/ReferencePage"));
const LogsPage = lazyPage(() => import("./pages/LogsPage"));
const NotificationsPage = lazyPage(() => import("./pages/NotificationsPage"));
const RateLimitsPage = lazyPage(() => import("./pages/RateLimitsPage"));
const ClassesPage = lazyPage(() => import("./pages/ClassesPage"));
const ExecutionInboxPage = lazyPage(() => import("./pages/ExecutionInboxPage"));

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
      <ConfirmProvider>
      <AdminStreamProvider>
        <BreadcrumbProvider>
          <AppShell>
            <ErrorBoundary>
              <Suspense fallback={<PageSpinner label="Загружаем страницу…" />}>
                <Outlet />
              </Suspense>
            </ErrorBoundary>
          </AppShell>
        </BreadcrumbProvider>
      </AdminStreamProvider>
      </ConfirmProvider>
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
      <Route path="/login" element={<Suspense fallback={<PageSpinner label="Загружаем вход…" />}><LoginPage /></Suspense>} />
      <Route element={<Private />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/monitoring" element={<MonitoringPage />} />
        <Route path="/grpc" element={<GrpcPage />} />
        <Route path="/rate-limits" element={<RateLimitsPage />} />
        <Route path="/classes" element={<ClassesPage />} />
        <Route path="/brokers" element={<BrokersPage />} />
        <Route path="/brokers/:id" element={<BrokerDetailPage />} />
        <Route path="/brokers/:id/tokens" element={<BrokerTokensRedirect />} />
        <Route path="/tokens" element={<TokensPage />} />
        <Route path="/orders" element={<OrdersPage />} />
        <Route path="/orders/:id" element={<OrderDetailPage />} />
        <Route path="/trades" element={<TradesPage />} />
        <Route path="/market-data" element={<MarketDataPage />} />
        <Route path="/market-data/:board/:symbol" element={<MarketInstrumentPage />} />
        <Route path="/quotes" element={<QuotesPage />} />
        <Route path="/positions" element={<PositionsPage />} />
        <Route path="/adapters" element={<AdaptersPage />} />
        <Route path="/reference" element={<ReferencePage />} />
        <Route path="/logs" element={<LogsPage />} />
        <Route path="/logs/:kind" element={<LogsPage />} />
        <Route path="/notifications" element={<NotificationsPage />} />
        <Route path="/execution-inbox" element={<ExecutionInboxPage />} />
      </Route>
    </Routes>
  );
}
