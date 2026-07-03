"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { formatBytes, formatDate } from "@/lib/format";
import { Button } from "./ui/Button";
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
    <div className="rounded-lg border border-border">
      <button onClick={toggle} className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-2">
        <span aria-hidden>📄</span>
        {renaming ? (
          <input
            className="flex-1 rounded border border-border bg-surface px-2 py-1 text-sm"
            value={newName}
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => setNewName(e.target.value)}
          />
        ) : (
          <span className="flex-1 truncate text-sm">{name}</span>
        )}
        {versions?.[0] && <span className="text-xs text-muted">{formatBytes(versions[0].size_bytes)}</span>}
      </button>

      {open && (
        <div className="border-t border-border px-4 py-3 text-sm">
          {error && <p className="mb-2 text-danger">{error}</p>}
          <div className="flex flex-wrap gap-2">
            <Button variant="ghost" disabled={busy} onClick={download}>
              Download
            </Button>
            {renaming ? (
              <Button variant="ghost" disabled={busy} onClick={rename}>
                Save name
              </Button>
            ) : (
              <Button variant="ghost" onClick={() => setRenaming(true)}>
                Rename
              </Button>
            )}
            <Button variant="ghost" disabled={busy} onClick={createShare}>
              Share
            </Button>
            <Button variant="danger" disabled={busy} onClick={trash}>
              Move to trash
            </Button>
          </div>

          {share && (
            <div className="mt-3 flex flex-wrap items-center gap-2 rounded-lg bg-surface-2 px-3 py-2">
              <input
                readOnly
                value={share.url}
                className="min-w-0 flex-1 truncate bg-transparent text-xs text-muted"
              />
              <Button variant="ghost" className="shrink-0" onClick={() => navigator.clipboard.writeText(share.url)}>
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

          <div className="mt-3">
            <div className="mb-1 text-xs uppercase tracking-wide text-muted">Version history</div>
            <ul className="flex flex-col gap-1">
              {versions?.map((v, i) => (
                <li key={v.id} className="flex items-center justify-between gap-2 text-xs text-muted">
                  <span>
                    {formatDate(v.created_at)} · {formatBytes(v.size_bytes)} · {v.mime_type}
                    {i === 0 && <span className="ml-2 text-accent">current</span>}
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
                      Restore this version
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
