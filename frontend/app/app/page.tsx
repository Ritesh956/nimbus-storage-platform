"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { Card, EyebrowLabel } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";

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
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-6 px-4">
      <EyebrowLabel>Nimbus</EyebrowLabel>
      {orgs && orgs.length > 0 && (
        <Card className="w-full">
          <h2 className="mb-3 text-lg font-semibold">Your organizations</h2>
          <ul className="flex flex-col gap-2">
            {orgs.map((o) => (
              <li key={o.id}>
                <button
                  onClick={() => router.push(`/app/org/${o.id}`)}
                  className="w-full rounded-lg border border-border px-3 py-2 text-left text-sm hover:border-border-strong hover:bg-surface-2"
                >
                  {o.name}
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}
      <Card className="w-full">
        <h2 className="mb-3 text-lg font-semibold">Create an organization</h2>
        <form onSubmit={createOrg} className="flex flex-col gap-3">
          <Input placeholder="Acme Inc." value={name} onChange={(e) => setName(e.target.value)} required />
          {error && <p className="text-sm text-danger">{error}</p>}
          <Button type="submit" disabled={creating}>
            {creating ? "Creating…" : "Create"}
          </Button>
        </form>
      </Card>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex min-h-screen items-center justify-center text-muted">{children}</div>;
}
