import { InputHTMLAttributes } from "react";
import { CheckIcon } from "./Icons";

// Dashdark X checkbox: the native input drives state/a11y/click-target but
// is visually hidden — a themed box (peer-checked → gradient fill + check
// glyph) replaces the browser's unthemeable default, which renders as a
// stark white square on this dark background.
export function Checkbox({ className = "", ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <span className={`relative inline-grid size-4 shrink-0 place-items-center ${className}`}>
      <input type="checkbox" className="peer absolute inset-0 size-full cursor-pointer opacity-0" {...props} />
      <span
        className="glow-ring pointer-events-none grid size-4 place-items-center rounded border border-border bg-surface-deep text-transparent transition-colors peer-checked:border-transparent peer-checked:bg-[linear-gradient(128deg,#cb3cff_19%,#7f25fb_68%)] peer-checked:text-white peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-accent"
        aria-hidden
      >
        <CheckIcon size={11} strokeWidth={3} />
      </span>
    </span>
  );
}
