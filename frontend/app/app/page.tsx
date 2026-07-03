"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { LogoMark, FolderIcon, PlusIcon } from "@/components/ui/Icons";

export default function AppHome() {
  const router = useRouter();
  const { data: orgs, isLoading, mutate } = useSWR("orgs", () => api.orgs.listMine());
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // router.replace() triggers a state update in the router — calling it
  // directly during render (rather than in an effect) updates a different
  // component while this one is still rendering, which React flags. The
  // sibling redirect in org/[orgId]/page.tsx already did this correctly;
  // this one didn't, found by actually clicking through the app.
  useEffect(() => {
    if (orgs && orgs.length === 1) {
      router.replace(`/app/org/${orgs[0].id}`);
    }
  }, [orgs, router]);

  if (isLoading || (orgs && orgs.length === 1)) return <Centered>Loading…</Centered>;

  async function createOrg(e: FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError(null);
    try {
      const org = await api.orgs.create(name);
      await mutate();
      router.replace(`/app/org/${org.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create organization");
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col items-center justify-center gap-6 px-4">
      <div className="flex flex-col items-center gap-3">
        <LogoMark size={44} />
        <h1 className="text-xl font-semibold tracking-tight">Choose a workspace</h1>
      </div>

      {orgs && orgs.length > 0 && (
        <Card className="w-full p-4">
          <div className="mb-2 px-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-2">
            Your organizations
          </div>
          <ul className="flex flex-col gap-1">
            {orgs.map((o) => (
              <li key={o.id}>
                <button
                  onClick={() => router.push(`/app/org/${o.id}`)}
                  className="glow-ring flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors hover:bg-surface-2"
                >
                  <span className="grid size-8 place-items-center rounded-lg bg-surface-deep text-accent">
                    <FolderIcon size={15} />
                  </span>
                  <span className="flex-1 truncate">{o.name}</span>
                  <span className="text-muted-2">→</span>
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <Card className="w-full p-4">
        <div className="mb-3 px-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-2">
          Create an organization
        </div>
        <form onSubmit={createOrg} className="flex gap-2">
          <Input placeholder="Acme Inc." value={name} onChange={(e) => setName(e.target.value)} required />
          <Button type="submit" disabled={creating} className="shrink-0">
            <PlusIcon size={13} />
            {creating ? "Creating…" : "Create"}
          </Button>
        </form>
        {error && <p className="mt-2 text-xs text-danger">{error}</p>}
      </Card>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex min-h-screen items-center justify-center text-sm text-muted">{children}</div>;
}
