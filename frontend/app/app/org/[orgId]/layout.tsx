"use client";

import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { AppShell } from "@/components/AppShell";

export default function OrgLayout({ children }: { children: React.ReactNode }) {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: orgs } = useSWR("orgs", () => api.orgs.listMine());
  const org = orgs?.find((o) => o.id === orgId);

  return (
    <AppShell orgId={orgId} orgName={org?.name}>
      {children}
    </AppShell>
  );
}
