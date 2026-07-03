"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { formatBytes, formatDate } from "@/lib/format";
import { Button } from "./ui/Button";
import {
  FileIcon,
  DownloadIcon,
  PencilIcon,
  LinkIcon,
  TrashIcon,
  CopyIcon,
  ChevronDownIcon,
  RestoreIcon,
} from "./ui/Icons";
import type { FileVersion, ShareLink } from "@/lib/types";

interface Props {
  fileId: string;
  name: string;
  onChanged: () => void;
}

export function FileRow({ fileId, name, onChanged }: Props) {
  const [open, setOpen] = useState(false);
  const [versions, setVersions] = useState<FileVersion[] | null>(null);
  const [share, setShare] = useState<ShareLink | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState(name);

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
      const link = await api.files.share(fileId);
      // The backend's own `url` field points at itself
      // (NIMBUS_API/v1/shares/{token}, raw JSON) since it has no idea
      // what the frontend's origin is — found by actually clicking
      // "Share" and looking at the result, not just reading the code.
      // The frontend knows its own origin, so it builds the link to its
      // own polished /shares/{token} page instead, using just the token.
      setShare({ ...link, url: `${window.location.origin}/shares/${link.token}` });
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
      <button
        onClick={toggle}
        className="glow-ring flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-2/60 sm:px-5"
      >
        <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent">
          <FileIcon size={15} />
        </span>
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
        {versions?.[0] && <span className="text-xs text-muted-2">{formatBytes(versions[0].size_bytes)}</span>}
        <ChevronDownIcon
          size={14}
          className={`shrink-0 text-muted-2 transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

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
            <Button variant="secondary" disabled={busy} onClick={createShare}>
              <LinkIcon size={13} />
              Share
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
              <Button variant="ghost" className="shrink-0" onClick={() => navigator.clipboard.writeText(share.url)}>
                <CopyIcon size={13} />
                Copy
              </Button>
              <Button
                variant="ghost"
                className="shrink-0"
                onClick={async () => {
                  await api.shares.revoke(share.token);
                  setShare(null);
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
    </li>
  );
}
