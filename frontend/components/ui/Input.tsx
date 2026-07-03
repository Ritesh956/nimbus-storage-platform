import { InputHTMLAttributes, forwardRef } from "react";

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className = "", ...props }, ref) {
    return (
      <input
        ref={ref}
        className={`glow-ring w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm text-foreground placeholder:text-muted focus:border-accent ${className}`}
        {...props}
      />
    );
  },
);
