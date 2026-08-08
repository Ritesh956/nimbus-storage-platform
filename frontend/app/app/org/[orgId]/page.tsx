"use client";

import { FormEvent, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { FolderIcon } from "@/components/ui/Icons";

export default function OrgHome() {
  const { orgId } = useParams<{ orgId: string }>();
  const router = useRouter();
  const { data: folders, isLoading, mutate } = useSWR(orgId ? ["root-folders", orgId] : null, () =>
    api.orgs.rootFolders(orgId),
  );

  useEffect(() => {
    if (folders && folders.length > 0) {
      router.replace(`/app/org/${orgId}/folder/${folders[0].id}`);
    }
  }, [folders, orgId, router]);

  if (isLoading || (folders && folders.length > 0)) {
    return <div className="text-sm text-muted">Loading…</div>;
  }

  // Rare fallback: the auto-created "Home" root folder (org.Service.Create)
  // is best-effort and could have failed — give the owner a way to recover.
  return (
    <div className="mx-auto max-w-sm">
      <Card>
        <EmptyState
          icon={<FolderIcon size={18} />}
          title="Welcome to your new org"
          description="You don't have any folders yet — create the first one to start uploading files."
          action={
            <CreateFolderForm
              onCreate={async (name) => {
                await api.folders.create(orgId, name, null);
                await mutate();
              }}
            />
          }
        />
      </Card>
    </div>
  );
}

function CreateFolderForm({ onCreate }: { onCreate: (name: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onCreate(name);
      setName("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create folder");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex gap-2">
      <Input
        aria-label="Folder name"
        placeholder="Folder name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        required
      />
      <Button type="submit" disabled={busy}>
        Create
      </Button>
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
    </form>
  );
}
