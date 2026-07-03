"use client";

import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/Button";

export default function TrashPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: folders, mutate: mutateFolders } = useSWR(["trash-folders", orgId], () => api.orgs.trashedFolders(orgId));
  const { data: files, mutate: mutateFiles } = useSWR(["trash-files", orgId], () => api.orgs.trashedFiles(orgId));

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <h1 className="text-xl font-semibold">Trash</h1>

      <div>
        <div className="mb-2 text-xs uppercase tracking-wide text-muted">Folders</div>
        {folders?.length === 0 && <p className="text-sm text-muted">Nothing here.</p>}
        <div className="flex flex-col gap-2">
          {folders?.map((f) => (
            <div key={f.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3 text-sm">
              <span>📁 {f.name}</span>
              <Button
                variant="ghost"
                onClick={async () => {
                  await api.folders.restore(f.id);
                  await mutateFolders();
                }}
              >
                Restore
              </Button>
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="mb-2 text-xs uppercase tracking-wide text-muted">Files</div>
        {files?.length === 0 && <p className="text-sm text-muted">Nothing here.</p>}
        <div className="flex flex-col gap-2">
          {files?.map((f) => (
            <div key={f.id} className="flex items-center justify-between rounded-lg border border-border px-4 py-3 text-sm">
              <span>📄 {f.name}</span>
              <div className="flex gap-2">
                <Button
                  variant="ghost"
                  onClick={async () => {
                    await api.files.restore(f.id);
                    await mutateFiles();
                  }}
                >
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
                  Delete forever
                </Button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
