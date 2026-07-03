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
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div className="flex items-center justify-between">
        <Link href={`/app/org/${orgId}`} className="text-sm text-muted hover:text-foreground">
          ← All folders
        </Link>
        <Button variant="ghost" onClick={() => setShowNewFolder((s) => !s)}>
          + New folder
        </Button>
      </div>

      {showNewFolder && (
        <form onSubmit={createFolder} className="flex gap-2">
          <Input
            autoFocus
            placeholder="Folder name"
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            required
          />
          <Button type="submit">Create</Button>
        </form>
      )}
      {error && <p className="text-sm text-danger">{error}</p>}

      <UploadDropzone folderId={folderId} onUploaded={() => mutate()} />

      {isLoading && <p className="text-sm text-muted">Loading…</p>}

      {data && data.folders.length > 0 && (
        <div>
          <div className="mb-2 text-xs uppercase tracking-wide text-muted">Folders</div>
          <div className="flex flex-col gap-2">
            {data.folders.map((f) => (
              <div key={f.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3">
                <Link href={`/app/org/${orgId}/folder/${f.id}`} className="flex-1 text-sm hover:text-accent">
                  📁 {f.name}
                </Link>
                <Button variant="ghost" onClick={() => trashFolder(f.id)}>
                  Trash
                </Button>
              </div>
            ))}
          </div>
        </div>
      )}

      {data && (
        <div>
          <div className="mb-2 text-xs uppercase tracking-wide text-muted">Files</div>
          {data.files.length === 0 ? (
            <p className="text-sm text-muted">No files here yet.</p>
          ) : (
            <div className="flex flex-col gap-2">
              {data.files.map((f) => (
                <FileRow key={f.id} fileId={f.id} name={f.name} onChanged={() => mutate()} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
