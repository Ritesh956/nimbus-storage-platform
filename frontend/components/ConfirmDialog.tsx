"use client";

import { useId, useState } from "react";
import { useModal } from "@/lib/useModal";
import { Button } from "./ui/Button";
import { TrashIcon } from "./ui/Icons";

interface Props {
  title: string;
  body: string;
  confirmLabel: string;
  onCancel: () => void;
  // May be async — the dialog shows a busy state and closes itself only
  // after the action resolves, so a failed purge doesn't silently vanish.
  onConfirm: () => Promise<void> | void;
}

// ConfirmDialog replaces window.confirm() for destructive actions — the
// native popup renders as a bare "localhost:3000 says…" box that looks
// nothing like the app. Same overlay/panel language as MoveDialog.
export function ConfirmDialog({ title, body, confirmLabel, onCancel, onConfirm }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const titleId = useId();
  const panelRef = useModal<HTMLDivElement>(onCancel);

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
    } catch (err) {
      setError(err instanceof Error ? err.message : "action failed");
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4 backdrop-blur-sm" onClick={onCancel}>
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="panel w-full max-w-sm overflow-hidden outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start gap-3 px-5 pt-5">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-danger/10 text-danger">
            <TrashIcon size={16} />
          </span>
          <div className="min-w-0">
            <h2 id={titleId} className="text-sm font-medium">{title}</h2>
            <p className="mt-1 text-xs leading-relaxed text-muted">{body}</p>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 px-5 py-4">
          {error && (
            <p role="alert" className="mr-auto min-w-0 truncate text-xs text-danger">
              {error}
            </p>
          )}
          <Button variant="secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant="danger" onClick={confirm} disabled={busy}>
            <TrashIcon size={13} />
            {busy ? "Deleting…" : confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
