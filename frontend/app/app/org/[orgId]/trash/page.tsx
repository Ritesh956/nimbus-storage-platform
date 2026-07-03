"use client";

import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/Button";
import { PageHeader } from "@/components/ui/PageHeader";
import { FolderIcon, FileIcon, RestoreIcon, TrashIcon } from "@/components/ui/Icons";

export default function TrashPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: folders, mutate: mutateFolders } = useSWR(["trash-folders", orgId], () => api.orgs.trashedFolders(orgId));
  const { data: files, mutate: mutateFiles } = useSWR(["trash-files", orgId], () => api.orgs.trashedFiles(orgId));

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Trash" description="Soft-deleted items — restore them, or delete files forever." />

      <div className="panel overflow-hidden">
        <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">Folders</div>
        {folders?.length === 0 ? (
          <p className="px-5 py-6 text-center text-xs text-muted-2">Nothing here.</p>
        ) : (
          <ul>
            {folders?.map((f) => (
              <li
                key={f.id}
                className="flex items-center gap-3 border-t border-border/40 px-5 py-3 first:border-t-0 hover:bg-surface-2/60"
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-muted-2">
                  <FolderIcon size={15} />
                </span>
                <span className="flex-1 truncate text-sm text-muted">{f.name}</span>
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
          <p className="px-5 py-6 text-center text-xs text-muted-2">Nothing here.</p>
        ) : (
          <ul>
            {files?.map((f) => (
              <li
                key={f.id}
                className="flex items-center gap-3 border-t border-border/40 px-5 py-3 first:border-t-0 hover:bg-surface-2/60"
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-muted-2">
                  <FileIcon size={15} />
                </span>
                <span className="flex-1 truncate text-sm text-muted">{f.name}</span>
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
                <Button
                  variant="danger"
                  onClick={async () => {
                    if (!confirm(`Permanently delete "${f.name}"? This cannot be undone.`)) return;
                    await api.files.purge(f.id);
                    await mutateFiles();
                  }}
                >
                  <TrashIcon size={13} />
                  Delete forever
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
