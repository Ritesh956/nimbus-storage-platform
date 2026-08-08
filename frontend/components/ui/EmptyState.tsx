import { ReactNode } from "react";

/* Shared empty-state layout — icon in a rounded well, a title, an optional
   description, and an optional action. Used anywhere a list can genuinely
   have zero items (new org, empty search, empty trash, empty folder) so
   those first-run moments look designed rather than a bare line of text. */
export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-5 py-10 text-center">
      <span className="grid size-10 place-items-center rounded-xl bg-surface-deep text-muted-2">{icon}</span>
      <p className="text-sm font-medium text-foreground">{title}</p>
      {description && <p className="max-w-xs text-xs text-muted-2">{description}</p>}
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
