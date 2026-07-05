"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Button } from "./ui/Button";
import { CopyIcon, FolderIcon, LinkIcon } from "./ui/Icons";
import { formatDate } from "@/lib/format";
import type { ShareLink } from "@/lib/types";

// Same presets as FileRow's inline single-file share — the API accepts any
// RFC3339 expires_at; these are just the sensible dropdown steps.
const expiryOptions = {
  never: { label: "No expiry", ms: 0 },
  "1h": { label: "Expires in 1 hour", ms: 60 * 60 * 1000 },
  "1d": { label: "Expires in 1 day", ms: 24 * 60 * 60 * 1000 },
  "7d": { label: "Expires in 7 days", ms: 7 * 24 * 60 * 60 * 1000 },
} as const;

type Target =
  | { kind: "folder"; id: string; name: string }
  | { kind: "files"; orgId: string; ids: string[] };

// ShareDialog creates the two newer share scopes: a whole folder, or a
// hand-picked multi-file bundle (one link instead of one per file). The
// single-file share stays inline in FileRow where it always lived.
export function ShareDialog({ target, onClose }: { target: Target; onClose: () => void }) {
  const [expiry, setExpiry] = useState<keyof typeof expiryOptions>("never");
  const [link, setLink] = useState<(ShareLink & { expiresAt: string | null }) | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function create() {
    setBusy(true);
    setError(null);
    try {
      const ttlMs = expiryOptions[expiry].ms;
      const expiresAt = ttlMs ? new Date(Date.now() + ttlMs).toISOString() : undefined;
      const created =
        target.kind === "folder"
          ? await api.folders.share(target.id, expiresAt)
          : await api.orgs.createBundleShare(target.orgId, target.ids, expiresAt);
      // Point at the frontend's own /shares page, not the backend's raw
      // JSON URL — same reasoning as FileRow.createShare.
      const url = `${window.location.origin}/shares/${created.token}`;
      setLink({ ...created, url, expiresAt: expiresAt ?? null });
      // Auto-copy so sharing is one click; the Copy button stays as the
      // fallback for when the clipboard write is refused (unfocused tab).
      try {
        await navigator.clipboard.writeText(url);
        setCopied(true);
      } catch {
        setCopied(false);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create share link");
    } finally {
      setBusy(false);
    }
  }

  const title =
    target.kind === "folder"
      ? `Share folder "${target.name}"`
      : `Share ${target.ids.length} file${target.ids.length === 1 ? "" : "s"}`;
  const description =
    target.kind === "folder"
      ? "Anyone with the link can browse this folder (subfolders included) and download its files."
      : "Anyone with the link can see and download exactly these files — one link for the whole set.";

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4 backdrop-blur-sm" onClick={onClose}>
      <div className="panel w-full max-w-md overflow-hidden" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start gap-3 border-b border-border/60 px-5 py-4">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
            {target.kind === "folder" ? <FolderIcon size={16} /> : <LinkIcon size={16} />}
          </span>
          <div className="min-w-0">
            <h2 className="truncate text-sm font-medium">{title}</h2>
            <p className="mt-1 text-xs leading-relaxed text-muted-2">{description}</p>
          </div>
        </div>

        <div className="flex flex-col gap-3 px-5 py-4">
          {!link && (
            <div className="flex items-center gap-2">
              <select
                value={expiry}
                onChange={(e) => setExpiry(e.target.value as keyof typeof expiryOptions)}
                className="glow-ring flex-1 rounded-lg border border-border bg-surface-deep px-2 py-2 text-xs text-muted focus:border-accent"
              >
                {Object.entries(expiryOptions).map(([key, opt]) => (
                  <option key={key} value={key}>
                    {opt.label}
                  </option>
                ))}
              </select>
              <Button disabled={busy} onClick={create} className="shrink-0">
                <LinkIcon size={13} />
                {busy ? "Creating…" : "Create link"}
              </Button>
            </div>
          )}

          {link && (
            <div className="flex flex-wrap items-center gap-2 rounded-lg border border-accent/25 bg-accent-soft px-3 py-2">
              <input readOnly value={link.url} className="min-w-0 flex-1 truncate bg-transparent text-xs text-muted" />
              <span className="shrink-0 text-[11px] text-muted-2">
                {link.expiresAt ? `expires ${formatDate(link.expiresAt)}` : "never expires"}
              </span>
              <Button
                variant="ghost"
                className="shrink-0"
                onClick={async () => {
                  await navigator.clipboard.writeText(link.url);
                  setCopied(true);
                }}
              >
                <CopyIcon size={13} />
                {copied ? "Copied" : "Copy"}
              </Button>
              <Button
                variant="ghost"
                className="shrink-0"
                onClick={async () => {
                  await api.shares.revoke(link.token);
                  setLink(null);
                  setCopied(false);
                }}
              >
                Revoke
              </Button>
            </div>
          )}

          {error && <p className="text-xs text-danger">{error}</p>}

          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              {link ? "Done" : "Cancel"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
