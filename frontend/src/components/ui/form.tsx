import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from "react";
import type { FieldError } from "react-hook-form";
import { cn } from "../../lib/utils";

/**
 * One labelled form control with its validation message. The control gets
 * the id, aria-invalid and aria-describedby, so screen readers read the error.
 */
export function Field({ label, error, hint, className, labelClassName, children }: {
  label: ReactNode;
  error?: FieldError | { message?: string };
  hint?: ReactNode;
  className?: string;
  labelClassName?: string;
  children: ReactElement<Record<string, unknown>>;
}) {
  const id = useId();
  const messageId = `${id}-message`;
  const message = error?.message;
  const control = isValidElement(children)
    ? cloneElement(children, {
      id: (children.props.id as string | undefined) ?? id,
      "aria-invalid": message ? true : undefined,
      "aria-describedby": message || hint ? messageId : undefined,
    })
    : children;
  return <div className={cn("space-y-1.5 text-sm", className)}>
    <label htmlFor={(children.props.id as string | undefined) ?? id} className={cn("block text-muted", labelClassName)}>{label}</label>
    {control}
    {message ? <p id={messageId} role="alert" className="text-xs text-danger">{message}</p>
      : hint ? <p id={messageId} className="text-xs text-faint">{hint}</p> : null}
  </div>;
}

/** The form-level error from the server (react-hook-form `errors.root`). */
export function FormError({ error }: { error?: { message?: string } }) {
  if (!error?.message) return null;
  return <p role="alert" className="rounded-md border border-danger/20 bg-danger-soft px-3 py-2 text-sm text-danger">{error.message}</p>;
}

export const errorText = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback;
