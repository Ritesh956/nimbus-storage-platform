"use client";

// A minimal toast queue for transient, dismissable feedback — currently used
// for the trash/delete undo affordance (roadmap #8): trashing a file or
// folder already calls the real restore endpoint, so undo here is just that
// same call fired from a toast instead of requiring a trip to the Trash
// page. Deliberately not a general notification-center pattern (no
// success/error variants) since nothing else in the app needs one yet.
import { createContext, useCallback, useContext, useRef, useState } from "react";
import { Button } from "./ui/Button";
import { CloseIcon } from "./ui/Icons";

interface ToastAction {
  label: string;
  onClick: () => void | Promise<void>;
}

interface ToastOptions {
  message: string;
  action?: ToastAction;
  // ms before auto-dismiss. Undo stops being offered once this fires.
  duration?: number;
}

interface Toast extends ToastOptions {
  id: number;
}

interface ToastContextValue {
  showToast: (options: ToastOptions) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

const DEFAULT_DURATION_MS = 6000;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(0);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  const dismiss = useCallback((id: number) => {
    const timer = timers.current.get(id);
    if (timer !== undefined) {
      clearTimeout(timer);
      timers.current.delete(id);
    }
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const showToast = useCallback(
    (options: ToastOptions) => {
      const id = nextId.current++;
      setToasts((prev) => [...prev, { ...options, id }]);
      timers.current.set(
        id,
        setTimeout(() => dismiss(id), options.duration ?? DEFAULT_DURATION_MS),
      );
    },
    [dismiss],
  );

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      {/* Always rendered (not conditional on toasts.length) so screen
          readers pick up the live region before the first toast lands. */}
      <div
        aria-live="polite"
        aria-atomic="false"
        className="pointer-events-none fixed inset-x-0 bottom-20 z-[60] flex flex-col items-center gap-2 px-4 lg:bottom-6"
      >
        {toasts.map((t) => (
          <ToastItem key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: (id: number) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function runAction() {
    if (!toast.action) return;
    setBusy(true);
    setError(null);
    try {
      await toast.action.onClick();
      onDismiss(toast.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "that didn't work");
      setBusy(false);
    }
  }

  return (
    <div
      role="status"
      className="panel pointer-events-auto flex w-full max-w-sm items-center gap-3 px-4 py-3 shadow-lg"
    >
      <span className={`min-w-0 flex-1 truncate text-xs ${error ? "text-danger" : "text-muted"}`}>
        {error ?? toast.message}
      </span>
      {toast.action && (
        <Button variant="ghost" className="shrink-0" disabled={busy} onClick={runAction}>
          {busy ? "…" : toast.action.label}
        </Button>
      )}
      <button
        onClick={() => onDismiss(toast.id)}
        title="Dismiss"
        aria-label="Dismiss notification"
        className="glow-ring shrink-0 rounded-lg p-1.5 text-muted-2 transition-colors hover:text-foreground"
      >
        <CloseIcon size={13} />
      </button>
    </div>
  );
}

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
