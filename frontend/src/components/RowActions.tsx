import { Icons } from "./icons";
import { useToast } from "./Toast";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "./ui/dropdown-menu";

export function RowActions({ label, actions }: { label: string; actions: { label: string; run: () => void | Promise<void>; danger?: boolean }[] }) {
  const toast = useToast();
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <button className="icon-btn h-8 w-8 data-[state=open]:bg-subtle data-[state=open]:text-ink" type="button" aria-label={label}>
          <Icons.more size={16} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" aria-label={label} className="w-52">
        {actions.map((action) => (
          <DropdownMenuItem
            key={action.label}
            variant={action.danger ? "destructive" : "default"}
            onSelect={() => {
              Promise.resolve().then(action.run).catch((error) => toast.push(error instanceof Error ? error.message : "Не удалось выполнить действие"));
            }}
          >
            {action.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
