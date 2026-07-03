import { HTMLAttributes } from "react";

export function Card({ className = "", ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={`panel p-5 ${className}`} {...props} />;
}

export function EyebrowLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-2">
      {children}
    </div>
  );
}

/* Dashdark-style KPI stat card: small muted label row, big semibold value,
   optional colored chip next to it. */
export function StatCard({
  label,
  value,
  icon,
  chip,
  chipTone = "success",
}: {
  label: string;
  value: React.ReactNode;
  icon?: React.ReactNode;
  chip?: React.ReactNode;
  chipTone?: "success" | "danger" | "neutral";
}) {
  const chipClasses = {
    success: "bg-success/10 text-success border-success/25",
    danger: "bg-danger/10 text-danger border-danger/25",
    neutral: "bg-surface-2 text-muted border-border",
  }[chipTone];
  return (
    <div className="panel flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2 text-muted-2">
        {icon}
        <span className="text-xs font-medium text-muted">{label}</span>
      </div>
      <div className="flex items-end gap-2">
        <span className="text-2xl font-semibold leading-none tracking-tight">{value}</span>
        {chip && (
          <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold leading-none ${chipClasses}`}>
            {chip}
          </span>
        )}
      </div>
    </div>
  );
}
