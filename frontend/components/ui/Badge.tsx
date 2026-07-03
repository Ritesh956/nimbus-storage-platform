// Dashdark X status pill: translucent tone background, hairline border,
// leading status dot, tiny radius.
export function Badge({
  children,
  tone = "neutral",
}: {
  children: React.ReactNode;
  tone?: "neutral" | "success" | "danger" | "warning";
}) {
  const toneClasses = {
    neutral: "bg-surface-2 text-muted border-border",
    success: "bg-success/10 text-success border-success/25",
    danger: "bg-danger/10 text-danger border-danger/25",
    warning: "bg-warning/10 text-warning border-warning/25",
  }[tone];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded border px-2 py-0.5 text-[11px] font-medium ${toneClasses}`}
    >
      <span className="size-1.5 rounded-full bg-current" aria-hidden />
      {children}
    </span>
  );
}
