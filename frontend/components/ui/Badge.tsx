export function Badge({
  children,
  tone = "neutral",
}: {
  children: React.ReactNode;
  tone?: "neutral" | "success" | "danger";
}) {
  const toneClasses = {
    neutral: "bg-surface-2 text-muted border-border",
    success: "bg-success/10 text-success border-success/30",
    danger: "bg-danger/10 text-danger border-danger/30",
  }[tone];
  return (
    <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${toneClasses}`}>
      {children}
    </span>
  );
}
