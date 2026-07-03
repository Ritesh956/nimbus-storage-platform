"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { UploadDropzone } from "@/components/UploadDropzone";
import { FileRow } from "@/components/FileRow";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PageHeader } from "@/components/ui/PageHeader";
import { ArrowLeftIcon, FileIcon, FolderIcon, PlusIcon, TrashIcon } from "@/components/ui/Icons";

export default function FolderPage() {
  const { orgId, folderId } = useParams<{ orgId: string; folderId: string }>();
  const { data, isLoading, mutate } = useSWR(folderId ? ["children", folderId] : null, () =>
    api.folders.children(folderId),
  );
  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [error, setError] = useState<string | null>(null);

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

  async function trashFolder(id: string) {
    await api.folders.trash(id);
    await mutate();
  }

  return (
    <div className="flex flex-col gap-5">
      <Link
        href={`/app/org/${orgId}`}
        className="glow-ring -mb-1 inline-flex w-fit items-center gap-1.5 rounded text-xs text-muted-2 transition-colors hover:text-foreground"
      >
        <ArrowLeftIcon size={13} />
        All folders
      </Link>

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
                  onClick={() => trashFolder(f.id)}
                  title="Move to trash"
                  className="glow-ring mx-3 rounded-lg p-2 text-muted-2 transition-all hover:bg-danger/10 hover:text-danger lg:opacity-0 lg:group-hover:opacity-100"
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
          <div className="flex items-center justify-between border-b border-border/60 px-4 py-3.5 sm:px-5">
            <span className="text-sm font-medium">Files</span>
            <span className="text-xs text-muted-2">{data.files.length}</span>
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
                <FileRow key={f.id} fileId={f.id} name={f.name} onChanged={() => mutate()} />
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
