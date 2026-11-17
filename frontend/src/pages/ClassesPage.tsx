import { PageHeader } from "../components/PageHeader";
import { MarketClassesSettings } from "../components/MarketClassesSettings";

// The global choice of QUIK classes; it limits every page and the broker API.
export default function ClassesPage() {
  return <div className="space-y-4">
    <PageHeader title="Классы QUIK" subtitle="Какие классы видны в Gateway: текущие торги, заявки, котировки, сделки, инструменты и брокерский API. Ничего не выбрано — все классы" />
    <MarketClassesSettings />
  </div>;
}
