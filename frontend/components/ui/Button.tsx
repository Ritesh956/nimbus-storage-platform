"use client";

import { ButtonHTMLAttributes, forwardRef } from "react";

type Variant = "primary" | "ghost" | "danger";

const variantClasses: Record<Variant, string> = {
  primary:
    "bg-accent-strong text-white hover:bg-accent border border-transparent shadow-[0_0_20px_-4px_var(--accent-strong)]",
  ghost:
    "bg-transparent text-foreground border border-border hover:border-border-strong hover:bg-surface-2",
  danger:
    "bg-transparent text-danger border border-danger/40 hover:bg-danger/10",
};

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }
>(function Button({ variant = "primary", className = "", disabled, ...props }, ref) {
  return (
    <button
      ref={ref}
      disabled={disabled}
      className={`glow-ring inline-flex items-center justify-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${variantClasses[variant]} ${className}`}
      {...props}
    />
  );
});
