import { HTMLAttributes } from "react";

export function Card({ className = "", ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={`glass-card p-5 ${className}`} {...props} />;
}

export function EyebrowLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-xs font-medium uppercase tracking-wide text-accent">
      {children}
    </div>
  );
}
