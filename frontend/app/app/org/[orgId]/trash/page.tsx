"use client";

import { useState } from "react";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/Button";
import { PageHeader } from "@/components/ui/PageHeader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FolderIcon, FileIcon, RestoreIcon, TrashIcon } from "@/components/ui/Icons";
import { EmptyState } from "@/components/ui/EmptyState";

export default function TrashPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: folders, mutate: mutateFolders } = useSWR(["trash-folders", orgId], () => api.orgs.trashedFolders(orgId));
  const { data: files, mutate: mutateFiles } = useSWR(["trash-files", orgId], () => api.orgs.trashedFiles(orgId));
  const [purging, setPurging] = useState<{ id: string; name: string } | null>(null);

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Trash" description="Soft-deleted items — restore them, or delete files forever." />

      <div className="panel overflow-hidden">
        <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">Folders</div>
        {folders?.length === 0 ? (
          <EmptyState
            icon={<FolderIcon size={18} />}
            title="No trashed folders"
            description="Folders you delete show up here until you restore or purge them."
          />
        ) : (
          <ul>
            {folders?.map((f) => (
              <li
                key={f.id}
                className="flex flex-wrap items-center gap-3 border-t border-border/40 px-4 py-3 first:border-t-0 hover:bg-surface-2/60 sm:px-5"
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-muted-2">
                  <FolderIcon size={15} />
                </span>
                <span className="min-w-32 flex-1 truncate text-sm text-muted">{f.name}</span>
                <Button
                  variant="secondary"
                  onClick={async () => {
                    await api.folders.restore(f.id);
                    await mutateFolders();
                  }}
                >
                  <RestoreIcon size={13} />
                  Restore
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="panel overflow-hidden">
        <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">Files</div>
        {files?.length === 0 ? (
          <EmptyState
            icon={<TrashIcon size={18} />}
            title="No trashed files"
            description="Files you delete show up here until you restore or delete them forever."
          />
        ) : (
          <ul>
            {files?.map((f) => (
              <li
                key={f.id}
                className="flex flex-wrap items-center gap-3 border-t border-border/40 px-4 py-3 first:border-t-0 hover:bg-surface-2/60 sm:px-5"
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-muted-2">
                  <FileIcon size={15} />
                </span>
                <span className="min-w-32 flex-1 truncate text-sm text-muted">{f.name}</span>
                <span className="ml-auto flex shrink-0 gap-2">
                  <Button
                    variant="secondary"
                    onClick={async () => {
                      await api.files.restore(f.id);
                      await mutateFiles();
                    }}
                  >
                    <RestoreIcon size={13} />
                    Restore
                  </Button>
                  <Button variant="danger" onClick={() => setPurging({ id: f.id, name: f.name })}>
                    <TrashIcon size={13} />
                    Delete forever
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {purging && (
        <ConfirmDialog
          title={`Delete "${purging.name}" forever?`}
          body="This permanently removes the file and every version of it. Freed storage is reclaimed by the garbage collector. This cannot be undone."
          confirmLabel="Delete forever"
          onCancel={() => setPurging(null)}
          onConfirm={async () => {
            await api.files.purge(purging.id);
            await mutateFiles();
            setPurging(null);
          }}
        />
      )}
    </div>
  );
}
