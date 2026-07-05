"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import JSZip from "jszip";
import { api, ApiError } from "@/lib/api";
import { Card, EyebrowLabel } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { LogoMark, FileIcon, FolderIcon, DownloadIcon, ArrowLeftIcon } from "@/components/ui/Icons";
import { formatBytes } from "@/lib/format";
import type { DownloadPlan, ResolvedShare, ShareFileInfo, ShareFolderInfo } from "@/lib/types";

// Deliberately outside app/app/ — this route is never wrapped in
// RequireAuth. That's the entire point of a share link
// (docs/06-api-design.md §7: GET /v1/shares/{token} is public).
//
// Three share kinds (post-Tier-3 session): a single file (plan embedded in
// the resolve), a folder (navigable listing, per-file plans on demand), and
// a multi-file bundle (flat listing, per-file plans on demand).
export default function SharePage() {
  const { token } = useParams<{ token: string }>();
  const [resolved, setResolved] = useState<ResolvedShare | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.shares
      .resolve(token)
      .then(setResolved)
      .catch((err) => setError(err instanceof ApiError ? err.message : "this link is invalid or has expired"));
  }, [token]);

  return (
    <div className="flex flex-1 items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        <div className="mb-6 flex flex-col items-center gap-3">
          <LogoMark size={44} />
          <EyebrowLabel>Shared via Nimbus</EyebrowLabel>
        </div>
        {error && (
          <Card className="p-8 text-center">
            <p className="text-sm text-danger">{error}</p>
          </Card>
        )}
        {!error && !resolved && (
          <Card className="p-8 text-center">
            <p className="text-sm text-muted">Loading…</p>
          </Card>
        )}
        {resolved?.kind === "file" && <SingleFile file={resolved.file} plan={resolved.download_plan} onError={setError} />}
        {resolved?.kind === "bundle" && <Bundle token={token} files={resolved.files} />}
        {resolved?.kind === "folder" && (
          <FolderBrowser token={token} root={resolved.folder} initialFolders={resolved.folders} initialFiles={resolved.files} />
        )}
      </div>
    </div>
  );
}

// assembleFromPlan reassembles a file client-side from presigned chunk
// URLs, primary replica first with fallback — same walk FileRow.download
// does.
async function assembleFromPlan(plan: DownloadPlan): Promise<Blob> {
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
  return new Blob(parts);
}

function saveBlob(blob: Blob, name: string) {
  const blobUrl = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = blobUrl;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(blobUrl);
}

async function downloadFromPlan(plan: DownloadPlan, name: string) {
  saveBlob(await assembleFromPlan(plan), name);
}

// zipFolderInto recursively walks a shared folder via the public children
// endpoint and adds every file to the zip, fetching each file's presigned
// plan just-in-time (they expire in 15 minutes — presigning a whole tree
// upfront would race the clock on big folders). The whole thing stays
// client-side: the server never touches file bytes on the download path
// (docs/01-srs.md FR-8), and zipping is no exception.
async function zipFolderInto(token: string, folderId: string, zip: JSZip, onFile: () => void) {
  const children = await api.shares.children(token, folderId);
  for (const f of children.files) {
    const { download_plan } = await api.shares.downloadPlan(token, f.id);
    zip.file(f.name, await assembleFromPlan(download_plan));
    onFile();
  }
  for (const sub of children.folders) {
    await zipFolderInto(token, sub.id, zip.folder(sub.name)!, onFile);
  }
}

function SingleFile({ file, plan, onError }: { file: ShareFileInfo; plan: DownloadPlan; onError: (m: string) => void }) {
  const [downloading, setDownloading] = useState(false);

  async function download() {
    setDownloading(true);
    try {
      await downloadFromPlan(plan, file.name);
    } catch (err) {
      onError(err instanceof Error ? err.message : "download failed");
    } finally {
      setDownloading(false);
    }
  }

  return (
    <Card className="p-8 text-center">
      <span className="mx-auto grid size-14 place-items-center rounded-2xl bg-surface-deep text-accent">
        <FileIcon size={26} />
      </span>
      <h1 className="mt-4 break-all text-lg font-semibold">{file.name}</h1>
      <p className="mt-1 text-xs text-muted-2">
        {formatBytes(file.size_bytes)} · {file.mime_type}
      </p>
      <Button className="mt-6 w-full py-2.5 text-sm" disabled={downloading} onClick={download}>
        <DownloadIcon size={15} />
        {downloading ? "Downloading…" : "Download"}
      </Button>
    </Card>
  );
}

// SharedFileRow is one downloadable file in a bundle or folder listing —
// its plan is fetched on click, not upfront (presigned URLs expire in 15
// minutes; a big listing shouldn't presign everything a visitor may never
// touch).
function SharedFileRow({ token, file }: { token: string; file: ShareFileInfo }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function download() {
    setBusy(true);
    setError(null);
    try {
      const { download_plan } = await api.shares.downloadPlan(token, file.id);
      await downloadFromPlan(download_plan, file.name);
    } catch (err) {
      setError(err instanceof Error ? err.message : "download failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="flex items-center gap-3 border-t border-border/40 px-4 py-3 first:border-t-0">
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent">
        <FileIcon size={15} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm">{file.name}</span>
        <span className="block text-[11px] text-muted-2">
          {formatBytes(file.size_bytes)}
          {error && <span className="ml-2 text-danger">{error}</span>}
        </span>
      </span>
      <Button variant="secondary" className="shrink-0" disabled={busy} onClick={download}>
        <DownloadIcon size={13} />
        {busy ? "…" : "Download"}
      </Button>
    </li>
  );
}

function Bundle({ token, files }: { token: string; files: ShareFileInfo[] }) {
  return (
    <Card className="overflow-hidden p-0">
      <div className="border-b border-border/60 px-5 py-4">
        <h1 className="text-sm font-medium">
          {files.length} shared file{files.length === 1 ? "" : "s"}
        </h1>
        <p className="mt-0.5 text-xs text-muted-2">One link, the whole set — download what you need.</p>
      </div>
      <ul>
        {files.length === 0 && <li className="px-5 py-6 text-center text-xs text-muted-2">Nothing shareable remains in this bundle.</li>}
        {files.map((f) => (
          <SharedFileRow key={f.id} token={token} file={f} />
        ))}
      </ul>
    </Card>
  );
}

function FolderBrowser({
  token,
  root,
  initialFolders,
  initialFiles,
}: {
  token: string;
  root: ShareFolderInfo;
  initialFolders: ShareFolderInfo[];
  initialFiles: ShareFileInfo[];
}) {
  // Path inside the share, root first; the last entry is what's on screen.
  const [path, setPath] = useState<ShareFolderInfo[]>([root]);
  const [folders, setFolders] = useState<ShareFolderInfo[]>(initialFolders);
  const [files, setFiles] = useState<ShareFileInfo[]>(initialFiles);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Zip-download progress: null = idle, number = files packed so far.
  const [zipped, setZipped] = useState<number | null>(null);

  async function open(target: ShareFolderInfo, nextPath: ShareFolderInfo[]) {
    setLoading(true);
    setError(null);
    try {
      const children = await api.shares.children(token, target.id);
      setPath(nextPath);
      setFolders(children.folders);
      setFiles(children.files);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to open folder");
    } finally {
      setLoading(false);
    }
  }

  const current = path[path.length - 1];

  // Downloads whatever folder is on screen — the whole share at the root,
  // just the subtree while browsing a subfolder.
  async function downloadCurrentFolder() {
    setZipped(0);
    setError(null);
    try {
      const zip = new JSZip();
      let n = 0;
      await zipFolderInto(token, current.id, zip, () => setZipped(++n));
      saveBlob(await zip.generateAsync({ type: "blob" }), `${current.name}.zip`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "folder download failed");
    } finally {
      setZipped(null);
    }
  }

  return (
    <Card className="overflow-hidden p-0">
      <div className="flex items-center gap-3 border-b border-border/60 px-5 py-4">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            {path.length > 1 && (
              <button
                onClick={() => open(path[path.length - 2], path.slice(0, -1))}
                className="glow-ring rounded p-1 text-muted-2 transition-colors hover:text-foreground"
                title="Up one level"
              >
                <ArrowLeftIcon size={14} />
              </button>
            )}
            <h1 className="min-w-0 truncate text-sm font-medium">{current.name}</h1>
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-2">{path.map((p) => p.name).join(" / ")}</p>
        </div>
        <Button variant="secondary" className="shrink-0" disabled={zipped !== null} onClick={downloadCurrentFolder}>
          <DownloadIcon size={13} />
          {zipped !== null ? `Zipping… (${zipped})` : "Download folder"}
        </Button>
      </div>
      {error && <p className="px-5 py-3 text-xs text-danger">{error}</p>}
      {loading && <p className="px-5 py-3 text-xs text-muted-2">Loading…</p>}
      <ul>
        {folders.map((f) => (
          <li key={f.id} className="border-t border-border/40 first:border-t-0">
            <button
              onClick={() => open(f, [...path, f])}
              className="glow-ring flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-2/60"
            >
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
                <FolderIcon size={15} />
              </span>
              <span className="truncate text-sm">{f.name}</span>
            </button>
          </li>
        ))}
        {files.map((f) => (
          <SharedFileRow key={f.id} token={token} file={f} />
        ))}
        {folders.length === 0 && files.length === 0 && !loading && (
          <li className="px-5 py-6 text-center text-xs text-muted-2">This folder is empty.</li>
        )}
      </ul>
    </Card>
  );
}
