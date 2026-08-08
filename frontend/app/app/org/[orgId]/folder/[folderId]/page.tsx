"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import useSWR, { useSWRConfig } from "swr";
import { api, ApiError } from "@/lib/api";
import { useLiveEvents } from "@/lib/live";
import { UploadDropzone } from "@/components/UploadDropzone";
import { FileRow } from "@/components/FileRow";
import { MoveDialog } from "@/components/MoveDialog";
import { ShareDialog } from "@/components/ShareDialog";
import { useToast } from "@/components/Toast";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PageHeader } from "@/components/ui/PageHeader";
import { FileIcon, FolderIcon, LinkIcon, PlusIcon, TrashIcon } from "@/components/ui/Icons";

export default function FolderPage() {
  const { orgId, folderId } = useParams<{ orgId: string; folderId: string }>();
  const { data, isLoading, mutate } = useSWR(folderId ? ["children", folderId] : null, () =>
    api.folders.children(folderId),
  );
  const { data: path } = useSWR(folderId ? ["path", folderId] : null, () => api.folders.path(folderId));
  const { mutate: mutateKey } = useSWRConfig();
  const { showToast } = useToast();
  // Live updates (backlog #12): a thumbnail_generated event re-fetches both
  // the children listing (has_thumbnail flips true) and that file's cached
  // thumbnail targets, so thumbs pop in without a refresh; an uploaded
  // event surfaces another member's upload into this listing.
  useLiveEvents(orgId, {
    onActivity: (e) => {
      if (e.verb === "thumbnail_generated") void mutateKey(["thumbnail", e.target_id]);
      if (e.verb === "thumbnail_generated" || e.verb === "uploaded") void mutate();
    },
  });
  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [movingFolder, setMovingFolder] = useState<{ id: string; name: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Multi-select for bundle sharing + folder-share dialog target.
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [shareTarget, setShareTarget] = useState<
    { kind: "folder"; id: string; name: string } | { kind: "files"; orgId: string; ids: string[] } | null
  >(null);

  // Selection doesn't survive navigation — React's documented
  // adjust-state-during-render pattern, not an effect.
  const [selectionFolder, setSelectionFolder] = useState(folderId);
  if (selectionFolder !== folderId) {
    setSelectionFolder(folderId);
    setSelected(new Set());
  }

  function toggleSelected(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function createFolder(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.folders.create(orgId, newFolderName, folderId);
      setNewFolderName("");
      setShowNewFolder(false);
      await mutate();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create folder");
    }
  }

  async function trashFolder(id: string, name: string) {
    await api.folders.trash(id);
    await mutate();
    showToast({
      message: `"${name}" moved to trash`,
      action: {
        label: "Undo",
        onClick: async () => {
          await api.folders.restore(id);
          await mutate();
        },
      },
    });
  }

  return (
    <div className="flex flex-col gap-5">
      {/* Breadcrumb trail — ancestors are links, the current folder is text. */}
      <nav aria-label="Breadcrumb" className="-mb-1 flex flex-wrap items-center gap-1 text-xs text-muted-2">
        {(path ?? []).map((p, i, arr) =>
          i < arr.length - 1 ? (
            <span key={p.id} className="flex items-center gap-1">
              <Link
                href={`/app/org/${orgId}/folder/${p.id}`}
                className="glow-ring rounded transition-colors hover:text-foreground"
              >
                {p.name}
              </Link>
              <span aria-hidden>/</span>
            </span>
          ) : (
            <span key={p.id} className="font-medium text-muted">
              {p.name}
            </span>
          ),
        )}
      </nav>

      <PageHeader
        title="Files"
        description="Chunked, deduplicated, and replicated across storage nodes."
        actions={
          <Button variant="secondary" onClick={() => setShowNewFolder((s) => !s)}>
            <PlusIcon size={13} />
            New folder
          </Button>
        }
      />

      {showNewFolder && (
        <form onSubmit={createFolder} className="-mt-3 flex gap-2">
          <Input
            autoFocus
            placeholder="Folder name"
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            required
          />
          <Button type="submit" className="shrink-0">
            Create
          </Button>
        </form>
      )}
      {error && <p className="-mt-3 text-xs text-danger">{error}</p>}

      <UploadDropzone folderId={folderId} onUploaded={() => mutate()} />

      {isLoading && <p className="text-xs text-muted">Loading…</p>}

      {data && data.folders.length > 0 && (
        <div className="panel overflow-hidden">
          <div className="flex items-center justify-between border-b border-border/60 px-4 py-3.5 sm:px-5">
            <span className="text-sm font-medium">Folders</span>
            <span className="text-xs text-muted-2">{data.folders.length}</span>
          </div>
          <ul>
            {data.folders.map((f) => (
              <li
                key={f.id}
                className="group flex items-center border-t border-border/40 transition-colors first:border-t-0 hover:bg-surface-2/60"
              >
                <Link
                  href={`/app/org/${orgId}/folder/${f.id}`}
                  className="glow-ring flex min-w-0 flex-1 items-center gap-3 py-3 pl-4 sm:pl-5"
                >
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
                    <FolderIcon size={15} />
                  </span>
                  <span className="truncate text-sm">{f.name}</span>
                </Link>
                <button
                  onClick={() => setShareTarget({ kind: "folder", id: f.id, name: f.name })}
                  title="Share folder"
                  className="glow-ring rounded-lg p-2 text-muted-2 transition-all hover:bg-surface-deep hover:text-foreground lg:opacity-0 lg:group-hover:opacity-100"
                >
                  <LinkIcon size={15} />
                </button>
                <button
                  onClick={() => setMovingFolder({ id: f.id, name: f.name })}
                  title="Move to…"
                  className="glow-ring rounded-lg p-2 text-muted-2 transition-all hover:bg-surface-deep hover:text-foreground lg:opacity-0 lg:group-hover:opacity-100"
                >
                  <FolderIcon size={15} />
                </button>
                <button
                  onClick={() => trashFolder(f.id, f.name)}
                  title="Move to trash"
                  className="glow-ring mr-3 rounded-lg p-2 text-muted-2 transition-all hover:bg-danger/10 hover:text-danger lg:opacity-0 lg:group-hover:opacity-100"
                >
                  <TrashIcon size={15} />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {data && (
        <div className="panel overflow-hidden">
          <div className="flex items-center justify-between gap-3 border-b border-border/60 px-4 py-3.5 sm:px-5">
            <span className="text-sm font-medium">Files</span>
            <span className="flex items-center gap-2">
              {selected.size > 0 && (
                <>
                  <Button
                    variant="secondary"
                    onClick={() => setShareTarget({ kind: "files", orgId, ids: [...selected] })}
                  >
                    <LinkIcon size={13} />
                    Share selected ({selected.size})
                  </Button>
                  <Button variant="ghost" onClick={() => setSelected(new Set())}>
                    Clear
                  </Button>
                </>
              )}
              <span className="text-xs text-muted-2">{data.files.length}</span>
            </span>
          </div>
          {data.files.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-5 py-10 text-center">
              <span className="grid size-10 place-items-center rounded-xl bg-surface-deep text-muted-2">
                <FileIcon size={18} />
              </span>
              <p className="text-xs text-muted-2">No files here yet — drop one above.</p>
            </div>
          ) : (
            <ul>
              {data.files.map((f) => (
                <FileRow
                  key={f.id}
                  file={f}
                  orgId={orgId}
                  folderId={folderId}
                  onChanged={() => mutate()}
                  selected={selected.has(f.id)}
                  onToggleSelect={() => toggleSelected(f.id)}
                />
              ))}
            </ul>
          )}
        </div>
      )}

      {movingFolder && (
        <MoveDialog
          orgId={orgId}
          item={{ kind: "folder", ...movingFolder }}
          onClose={() => setMovingFolder(null)}
          onMoved={() => mutate()}
        />
      )}

      {shareTarget && <ShareDialog target={shareTarget} onClose={() => setShareTarget(null)} />}
    </div>
  );
}
