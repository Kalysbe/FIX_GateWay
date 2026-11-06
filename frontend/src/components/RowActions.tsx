import { Icons } from "./icons";
import { useToast } from "./Toast";
import { useConfirm, type ConfirmOptions } from "./confirm";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "./ui/dropdown-menu";

export type RowAction = {
  label: string;
  run: () => void | Promise<unknown>;
  danger?: boolean;
  /** Toast after the action succeeds. */
  success?: string;
  /** Ask first; the action then runs from the dialog. */
  confirm?: Omit<ConfirmOptions, "action" | "success">;
};

export function RowActions({ label, actions }: { label: string; actions: RowAction[] }) {
  const toast = useToast();
  const ask = useConfirm();
  async function run(action: RowAction) {
    if (action.confirm) {
      await ask({ ...action.confirm, action: async () => { await action.run(); }, success: action.success });
      return;
    }
    try {
      await action.run();
      if (action.success) toast.push(action.success, "ok");
    } catch (error) {
      toast.push(error instanceof Error ? error.message : "Не удалось выполнить действие");
    }
  }
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
            // Let the menu close before a dialog takes focus.
            onSelect={() => { window.setTimeout(() => void run(action), 0); }}
          >
            {action.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
