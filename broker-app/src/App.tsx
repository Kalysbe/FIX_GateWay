import { useDesk } from "./hooks/useDesk";
import BrokerSidebar from "./components/BrokerSidebar";
import ConcurrentPanel from "./components/ConcurrentPanel";
import OrderForm from "./components/OrderForm";
import OrdersTable from "./components/OrdersTable";
import SettingsPanel from "./components/SettingsPanel";

export default function App() {
  const { state, connectedCount, activeBroker } = useDesk();

  return (
    <div className="min-h-screen">
      <header className="border-b border-white/10 bg-black/25">
        <div className="mx-auto flex max-w-[1400px] flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div>
            <div className="font-display text-xl font-bold tracking-tight text-accent">Broker Desk</div>
            <div className="text-xs text-slate-400">
              Несколько брокерских сессий по API-токенам → FIX Gateway
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-4 text-sm">
            <div className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5">
              <span className="text-slate-400">sessions </span>
              <span className="font-mono">{state.brokers.length}</span>
            </div>
            <div className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5">
              <span className="text-slate-400">online </span>
              <span className="font-mono text-buy">{connectedCount}</span>
            </div>
            <div className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5">
              <span className="text-slate-400">orders </span>
              <span className="font-mono">{state.orders.length}</span>
            </div>
            {activeBroker && (
              <div className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5">
                <span className="mr-2 inline-block h-2 w-2 rounded-full" style={{ background: activeBroker.color }} />
                <span className="font-mono text-xs">{activeBroker.code}</span>
              </div>
            )}
          </div>
        </div>
      </header>

      <main className="mx-auto grid max-w-[1400px] gap-4 px-4 py-5 lg:grid-cols-[320px_1fr]">
        <div className="min-h-[520px]">
          <BrokerSidebar />
        </div>
        <div className="space-y-4">
          <OrderForm />
          <ConcurrentPanel />
          <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr]">
            <OrdersTable />
            <SettingsPanel />
          </div>
        </div>
      </main>
    </div>
  );
}
