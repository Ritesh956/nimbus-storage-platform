"use client";

import { FormEvent, useState } from "react";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { formatBytes, timeAgo } from "@/lib/format";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PageHeader } from "@/components/ui/PageHeader";
import { StatCard } from "@/components/ui/Card";
import { TablePanel, Th, Td, Tr } from "@/components/ui/Table";
import { UsersIcon, TrashIcon, PlusIcon, FileIcon, LinkIcon, PulseIcon } from "@/components/ui/Icons";

export default function MembersPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: members, mutate } = useSWR(["members", orgId], () => api.orgs.listMembers(orgId));
  // Needed to tell the org's creator apart: the backend refuses to remove
  // them (ErrCannotRemoveOwner), so don't offer a button that can only fail.
  const { data: orgs } = useSWR("orgs", () => api.orgs.listMine());
  const creatorId = orgs?.find((o) => o.id === orgId)?.owner_user_id;
  // Owner-gated oversight — members get a 403 here, which just means no
  // panel (the fetcher swallows it rather than surfacing an error for a
  // view they were never meant to see).
  const { data: usage } = useSWR(
    ["org-usage", orgId],
    () => api.orgs.usage(orgId).catch((err) => (err instanceof ApiError && err.status === 403 ? null : Promise.reject(err))),
    { shouldRetryOnError: false },
  );
  const usageByUser = new Map((usage?.members ?? []).map((m) => [m.user_id, m]));
  const activityTotal = usage ? Object.values(usage.activity_30d).reduce((a, b) => a + b, 0) : 0;
  const quotaPct = usage && usage.storage.quota_bytes > 0
    ? Math.round((usage.storage.used_bytes / usage.storage.quota_bytes) * 100)
    : 0;

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"owner" | "member">("member");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Invite is owner-gated server-side; rather than guessing the caller's
  // role client-side, the form is always shown and a 403 is surfaced as a
  // readable message.
  async function invite(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.orgs.addMember(orgId, email, role);
      setEmail("");
      await mutate();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setError("Only organization owners can add members.");
      } else if (err instanceof ApiError && err.status === 404) {
        setError("No registered user with that email.");
      } else {
        setError(err instanceof ApiError ? err.message : "failed to add member");
      }
    } finally {
      setBusy(false);
    }
  }

  async function remove(userId: string) {
    setError(null);
    try {
      await api.orgs.removeMember(orgId, userId);
      await mutate();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setError("Only organization owners can remove members.");
      } else {
        setError(err instanceof ApiError ? err.message : "failed to remove member");
      }
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Members"
        description="People with access to this organization. Adding requires an owner role and an already-registered email."
      />

      {usage && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            label="Storage used"
            value={formatBytes(usage.storage.used_bytes)}
            icon={<FileIcon size={14} />}
            chip={`${quotaPct}% of ${formatBytes(usage.storage.quota_bytes)}`}
            chipTone={quotaPct >= 90 ? "danger" : quotaPct >= 70 ? "neutral" : "success"}
          />
          <StatCard
            label="Files"
            value={usage.storage.live_files}
            icon={<FileIcon size={14} />}
            chip={`${usage.storage.trashed_files} in trash`}
            chipTone="neutral"
          />
          <StatCard label="Active share links" value={usage.active_share_links} icon={<LinkIcon size={14} />} />
          <StatCard
            label="Actions (30d)"
            value={activityTotal}
            icon={<PulseIcon size={14} />}
            chip={`${usage.activity_30d["uploaded"] ?? 0} uploads`}
            chipTone="neutral"
          />
        </div>
      )}

      <form onSubmit={invite} className="flex flex-col gap-2 sm:flex-row">
        <Input
          type="email"
          required
          placeholder="teammate@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <select
          value={role}
          onChange={(e) => setRole(e.target.value as "owner" | "member")}
          className="glow-ring rounded-lg border border-border bg-surface-deep px-3 py-2 text-sm text-foreground focus:border-accent"
        >
          <option value="member">Member</option>
          <option value="owner">Owner</option>
        </select>
        <Button type="submit" disabled={busy || !email} className="shrink-0">
          <PlusIcon size={13} />
          Add member
        </Button>
      </form>
      {error && <p className="-mt-2 text-xs text-danger">{error}</p>}

      <TablePanel title={members ? `${members.length} member${members.length === 1 ? "" : "s"}` : "Members"}>
        <thead>
          <tr>
            <Th>Email</Th>
            <Th>Role</Th>
            {usage && <Th>Last active</Th>}
            {usage && <Th>Actions (30d)</Th>}
            <Th className="text-right">Actions</Th>
          </tr>
        </thead>
        <tbody>
          {members?.map((m) => {
            const stats = usageByUser.get(m.user_id);
            return (
              <Tr key={m.user_id}>
                <Td className="font-medium">
                  <span className="flex items-center gap-2.5">
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
                      <UsersIcon size={15} />
                    </span>
                    <span className="min-w-0 truncate">{m.email}</span>
                  </span>
                </Td>
                <Td>
                  <span className="inline-flex items-center gap-2">
                    <Badge tone={m.role === "owner" ? "success" : "neutral"}>{m.role}</Badge>
                    {m.user_id === creatorId && (
                      <span className="text-[11px] text-muted-2">creator</span>
                    )}
                  </span>
                </Td>
                {usage && (
                  <Td className="text-xs text-muted-2">
                    {stats?.last_active_at ? timeAgo(stats.last_active_at) : "never"}
                  </Td>
                )}
                {usage && <Td className="text-xs text-muted-2">{stats?.events_30d ?? 0}</Td>}
                <Td className="text-right">
                  {m.user_id !== creatorId && (
                    <button
                      onClick={() => remove(m.user_id)}
                      title="Remove member"
                      className="glow-ring rounded-lg p-2 text-muted-2 transition-colors hover:bg-danger/10 hover:text-danger"
                    >
                      <TrashIcon size={15} />
                    </button>
                  )}
                </Td>
              </Tr>
            );
          })}
          {members && members.length === 0 && (
            <Tr>
              <Td className="text-xs text-muted-2">No members yet.</Td>
              <Td />
              {usage && <Td />}
              {usage && <Td />}
              <Td />
            </Tr>
          )}
        </tbody>
      </TablePanel>
    </div>
  );
}
