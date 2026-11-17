import { PageHeader } from "../components/PageHeader";
import { ExecutionInbox } from "../components/ExecutionInbox";

export default function ExecutionInboxPage() {
  return <div className="space-y-4">
    <PageHeader title="Входящие FIX" subtitle="Очередь отчётов исполнения и карантин отчётов, которые нельзя применить автоматически" />
    <ExecutionInbox />
  </div>;
}
