"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { formatBytes, formatDate } from "@/lib/format";
import { Button } from "./ui/Button";
import { MoveDialog } from "./MoveDialog";
import {
  FolderIcon,
  FileIcon,
  DownloadIcon,
  PencilIcon,
  LinkIcon,
  TrashIcon,
  CopyIcon,
  ChevronDownIcon,
  RestoreIcon,
} from "./ui/Icons";
import type { FileSummary, FileVersion, ShareLink } from "@/lib/types";

// Thumb renders a file's worker-generated thumbnail in the row's icon slot,
// walking the presigned targets in order on load failure — the same
// primary+fallback replica logic download() uses for chunks, just driven by
// <img> onError instead of fetch. Falls back to the generic icon when every
// replica fails.
function Thumb({ fileId, name, onPreview }: { fileId: string; name: string; onPreview: (url: string) => void }) {
  const { data } = useSWR(["thumbnail", fileId], () => api.files.thumbnail(fileId));
  const [targetIdx, setTargetIdx] = useState(0);

  const url = data?.targets[targetIdx];
  if (!url) {
    return (
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent">
        <FileIcon size={15} />
      </span>
    );
  }
  return (
    <span
      role="button"
      title="Preview"
      onClick={(e) => {
        e.stopPropagation();
        onPreview(url);
      }}
      className="glow-ring block size-8 shrink-0 overflow-hidden rounded-lg bg-surface-deep"
    >
      {/* eslint-disable-next-line @next/next/no-img-element -- presigned MinIO URL, not optimizable */}
      <img
        src={url}
        alt={`Thumbnail of ${name}`}
        className="size-full object-cover"
        onError={() => setTargetIdx((i) => i + 1)}
      />
    </span>
  );
}

// PreviewModal shows the thumbnail at full size (256px longest edge — what
// the worker generates) over a dismissable backdrop.
function PreviewModal({ url, name, onClose }: { url: string; name: string; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="panel flex max-w-lg flex-col gap-3 p-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* eslint-disable-next-line @next/next/no-img-element -- presigned MinIO URL, not optimizable */}
        <img src={url} alt={`Preview of ${name}`} className="max-h-[70vh] rounded-lg object-contain" />
        <div className="flex items-center justify-between gap-3">
          <span className="min-w-0 truncate text-sm text-muted">{name}</span>
          <Button variant="secondary" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}

// Share-link expiry presets — the API accepts any RFC3339 expires_at; these
// are just the sensible dropdown steps (never = omit the field).
const expiryOptions = {
  never: { label: "No expiry", ms: 0 },
  "1h": { label: "Expires in 1 hour", ms: 60 * 60 * 1000 },
  "1d": { label: "Expires in 1 day", ms: 24 * 60 * 60 * 1000 },
  "7d": { label: "Expires in 7 days", ms: 7 * 24 * 60 * 60 * 1000 },
} as const;

interface Props {
  file: FileSummary;
  orgId: string;
  folderId: string;
  onChanged: () => void;
  // Multi-select for bundle sharing (post-Tier-3 session): when
  // onToggleSelect is provided the row renders a leading checkbox; the
  // parent owns the selection set.
  selected?: boolean;
  onToggleSelect?: () => void;
}

export function FileRow({ file, orgId, folderId, onChanged, selected = false, onToggleSelect }: Props) {
  const fileId = file.id;
  const name = file.name;
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState<string | null>(null);
  const [moving, setMoving] = useState(false);
  const [versions, setVersions] = useState<FileVersion[] | null>(null);
  const [share, setShare] = useState<ShareLink | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState(name);
  const [shareExpiry, setShareExpiry] = useState<keyof typeof expiryOptions>("never");
  const [shareExpiresAt, setShareExpiresAt] = useState<string | null>(null);
  const [shareCopied, setShareCopied] = useState(false);

  async function loadVersions() {
    try {
      setVersions(await api.files.versions(fileId));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load versions");
    }
  }

  async function toggle() {
    if (!open && versions === null) await loadVersions();
    setOpen((o) => !o);
  }

  // Reassembles the file client-side from the download plan's presigned
  // chunk URLs (docs/06-api-design.md §6) — the same primary+fallback
  // logic the smoke tests exercise, just triggered by a click instead of
  // a script.
  async function download() {
    setBusy(true);
    setError(null);
    try {
      const vs = versions ?? (await api.files.versions(fileId));
      if (!vs.length) throw new Error("no versions to download");
      const plan = await api.files.downloadPlan(fileId, vs[0].id);
      const parts: BlobPart[] = [];
      for (const chunk of [...plan.chunks].sort((a, b) => a.sequence - b.sequence)) {
        let ok = false;
        for (const url of chunk.targets) {
          const res = await fetch(url);
          if (res.ok) {
            parts.push(await res.blob());
            ok = true;
            break;
          }
        }
        if (!ok) throw new Error(`could not fetch chunk ${chunk.sequence} from any replica`);
      }
      const blobUrl = URL.createObjectURL(new Blob(parts));
      const a = document.createElement("a");
      a.href = blobUrl;
      a.download = name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(blobUrl);
    } catch (err) {
      setError(err instanceof Error ? err.message : "download failed");
    } finally {
      setBusy(false);
    }
  }

  async function createShare() {
    setBusy(true);
    setError(null);
    try {
      const ttlMs = expiryOptions[shareExpiry].ms;
      const expiresAt = ttlMs ? new Date(Date.now() + ttlMs).toISOString() : undefined;
      const link = await api.files.share(fileId, expiresAt);
      setShareExpiresAt(expiresAt ?? null);
      // The backend's own `url` field points at itself
      // (NIMBUS_API/v1/shares/{token}, raw JSON) since it has no idea
      // what the frontend's origin is — found by actually clicking
      // "Share" and looking at the result, not just reading the code.
      // The frontend knows its own origin, so it builds the link to its
      // own polished /shares/{token} page instead, using just the token.
      const url = `${window.location.origin}/shares/${link.token}`;
      setShare({ ...link, url });
      // Auto-copy so sharing is one click; the Copy button stays as the
      // fallback for when the clipboard write is refused (unfocused tab).
      try {
        await navigator.clipboard.writeText(url);
        setShareCopied(true);
      } catch {
        setShareCopied(false);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create share link");
    } finally {
      setBusy(false);
    }
  }

  async function trash() {
    setBusy(true);
    setError(null);
    try {
      await api.files.trash(fileId);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to trash file");
      setBusy(false);
    }
  }

  async function rename() {
    setBusy(true);
    setError(null);
    try {
      await api.files.rename(fileId, newName);
      setRenaming(false);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to rename file");
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="border-t border-border/40 first:border-t-0">
      {/* Checkbox lives beside the row button, not inside it — interactive
          content can't nest in a <button>. */}
      <div className="flex items-center">
        {onToggleSelect && (
          <label className="shrink-0 cursor-pointer py-3 pl-4 sm:pl-5" title="Select for sharing">
            <input
              type="checkbox"
              checked={selected}
              onChange={onToggleSelect}
              className="glow-ring size-3.5 cursor-pointer rounded"
              style={{ accentColor: "var(--accent)" }}
            />
          </label>
        )}
        <button
          onClick={toggle}
          className={`glow-ring flex w-full min-w-0 items-center gap-3 py-3 pr-4 text-left transition-colors hover:bg-surface-2/60 sm:pr-5 ${onToggleSelect ? "pl-3" : "pl-4 sm:pl-5"}`}
        >
        {file.has_thumbnail ? (
          <Thumb fileId={fileId} name={name} onPreview={setPreview} />
        ) : (
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent">
            <FileIcon size={15} />
          </span>
        )}
        {renaming ? (
          <input
            className="glow-ring flex-1 rounded-lg border border-border bg-surface-deep px-2 py-1 text-sm"
            value={newName}
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => setNewName(e.target.value)}
          />
        ) : (
          <span className="min-w-0 flex-1 truncate text-sm">{name}</span>
        )}
        {file.size_bytes != null && <span className="text-xs text-muted-2">{formatBytes(file.size_bytes)}</span>}
        <ChevronDownIcon
          size={14}
          className={`shrink-0 text-muted-2 transition-transform ${open ? "rotate-180" : ""}`}
        />
        </button>
      </div>

      {open && (
        <div className="border-t border-border/40 bg-surface-deep/40 px-4 py-4 text-sm sm:px-5">
          {error && <p className="mb-3 text-xs text-danger">{error}</p>}
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" disabled={busy} onClick={download}>
              <DownloadIcon size={13} />
              Download
            </Button>
            {renaming ? (
              <Button variant="secondary" disabled={busy} onClick={rename}>
                Save name
              </Button>
            ) : (
              <Button variant="secondary" onClick={() => setRenaming(true)}>
                <PencilIcon size={13} />
                Rename
              </Button>
            )}
            <span className="inline-flex items-center gap-1">
              <Button variant="secondary" disabled={busy} onClick={createShare}>
                <LinkIcon size={13} />
                Share
              </Button>
              <select
                value={shareExpiry}
                onChange={(e) => setShareExpiry(e.target.value as keyof typeof expiryOptions)}
                title="Share link expiry"
                className="glow-ring rounded-lg border border-border bg-surface-deep px-2 py-1.5 text-xs text-muted focus:border-accent"
              >
                {Object.entries(expiryOptions).map(([key, opt]) => (
                  <option key={key} value={key}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </span>
            <Button variant="secondary" disabled={busy} onClick={() => setMoving(true)}>
              <FolderIcon size={13} />
              Move
            </Button>
            <Button variant="danger" disabled={busy} onClick={trash}>
              <TrashIcon size={13} />
              Move to trash
            </Button>
          </div>

          {share && (
            <div className="mt-3 flex flex-wrap items-center gap-2 rounded-lg border border-accent/25 bg-accent-soft px-3 py-2">
              <input
                readOnly
                value={share.url}
                className="min-w-0 flex-1 truncate bg-transparent text-xs text-muted"
              />
              <span className="shrink-0 text-[11px] text-muted-2">
                {shareExpiresAt ? `expires ${formatDate(shareExpiresAt)}` : "never expires"}
              </span>
              <Button
                variant="ghost"
                className="shrink-0"
                onClick={async () => {
                  await navigator.clipboard.writeText(share.url);
                  setShareCopied(true);
                }}
              >
                <CopyIcon size={13} />
                {shareCopied ? "Copied" : "Copy"}
              </Button>
              <Button
                variant="ghost"
                className="shrink-0"
                onClick={async () => {
                  await api.shares.revoke(share.token);
                  setShare(null);
                  setShareCopied(false);
                }}
              >
                Revoke
              </Button>
            </div>
          )}

          <div className="mt-4">
            <div className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
              Version history
            </div>
            <ul className="flex flex-col gap-1.5">
              {versions?.map((v, i) => (
                <li key={v.id} className="flex items-center justify-between gap-2 text-xs text-muted">
                  <span>
                    {formatDate(v.created_at)} · {formatBytes(v.size_bytes)} · {v.mime_type}
                    {i === 0 && (
                      <span className="ml-2 rounded border border-success/25 bg-success/10 px-1.5 py-0.5 text-[10px] font-semibold text-success">
                        current
                      </span>
                    )}
                  </span>
                  {i !== 0 && (
                    <Button
                      variant="ghost"
                      onClick={async () => {
                        await api.files.restoreVersion(fileId, v.id);
                        setVersions(null);
                        await loadVersions();
                        onChanged();
                      }}
                    >
                      <RestoreIcon size={12} />
                      Restore
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      {preview && <PreviewModal url={preview} name={name} onClose={() => setPreview(null)} />}
      {moving && (
        <MoveDialog
          orgId={orgId}
          item={{ kind: "file", id: fileId, name, currentFolderId: folderId }}
          onClose={() => setMoving(false)}
          onMoved={onChanged}
        />
      )}
    </li>
  );
}
