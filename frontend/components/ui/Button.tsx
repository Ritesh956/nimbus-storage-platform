"use client";

import { ButtonHTMLAttributes, forwardRef } from "react";

type Variant = "primary" | "secondary" | "ghost" | "danger";

// Dashdark X button language: primary is the magenta→violet gradient with a
// soft glow; secondary is a bordered navy chip; ghost is borderless for
// inline row actions; danger mirrors ghost in the System Red.
const variantClasses: Record<Variant, string> = {
  primary:
    "gradient-primary text-white border border-transparent shadow-[0_0_16px_rgba(203,60,255,0.35)] hover:brightness-110",
  secondary:
    "bg-surface-deep text-muted border border-border hover:text-foreground hover:border-border-strong",
  ghost: "bg-transparent text-muted border border-transparent hover:text-foreground hover:bg-surface-2",
  danger: "bg-transparent text-danger border border-danger/30 hover:bg-danger/10",
};

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }
>(function Button({ variant = "primary", className = "", disabled, ...props }, ref) {
  return (
    <button
      ref={ref}
      disabled={disabled}
      className={`glow-ring inline-flex items-center justify-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-[color,background-color,border-color,filter] disabled:cursor-not-allowed disabled:opacity-50 ${variantClasses[variant]} ${className}`}
      {...props}
    />
  );
});
