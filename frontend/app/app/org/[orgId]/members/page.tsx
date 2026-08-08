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
import type { OrgRole } from "@/lib/types";

// Badge tones for the three-tier role ladder.
const roleTone = { owner: "success", admin: "warning", member: "neutral" } as const;

export default function MembersPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: members, mutate } = useSWR(["members", orgId], () => api.orgs.listMembers(orgId));
  // Needed to tell the org's creator apart: the backend refuses to remove
  // them (ErrCannotRemoveOwner), so don't offer a button that can only fail.
  const { data: orgs } = useSWR("orgs", () => api.orgs.listMine());
  const creatorId = orgs?.find((o) => o.id === orgId)?.owner_user_id;
  // The caller's own role decides which management controls to render at
  // all — the server still enforces every rule; this just avoids offering
  // buttons that can only 403 (same reasoning as the creator check above).
  const { data: me } = useSWR("me", () => api.auth.me(), { shouldRetryOnError: false });
  const myRole = members?.find((m) => m.user_id === me?.user_id)?.role;
  const canManage = myRole === "owner" || myRole === "admin";
  // Admins can only remove plain members; owners can remove anyone but the creator.
  const canRemove = (m: { user_id: string; role: OrgRole }) =>
    m.user_id !== creatorId && (myRole === "owner" || (myRole === "admin" && m.role === "member"));
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
  const [role, setRole] = useState<OrgRole>("member");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The form is only rendered for admin-and-up (canManage), and the role
  // options match what the caller may actually grant — but the server
  // still enforces everything, so a 403 stays readable rather than
  // impossible-by-construction.
  async function invite(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.orgs.addMember(orgId, email, role);
      setEmail("");
      await mutate();
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
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
      setError(err instanceof ApiError ? err.message : "failed to remove member");
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Members"
        description="People with access to this organization, and — for owners and admins — how it's being used."
      />

      {usage && (
        <div className="flex flex-col gap-2">
          <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-2">
            Organization usage
          </div>
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
        </div>
      )}

      {canManage && (
        <form onSubmit={invite} className="flex flex-col gap-2 sm:flex-row">
          <Input
            type="email"
            required
            aria-label="Teammate email"
            placeholder="teammate@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <select
            aria-label="Role"
            value={role}
            onChange={(e) => setRole(e.target.value as OrgRole)}
            className="glow-ring rounded-lg border border-border bg-surface-deep px-3 py-2 text-sm text-foreground focus:border-accent"
          >
            <option value="member">Member</option>
            {/* Elevated roles are owner-grantable only (the server rejects
                an admin trying — these options just aren't offered). */}
            {myRole === "owner" && <option value="admin">Admin</option>}
            {myRole === "owner" && <option value="owner">Owner</option>}
          </select>
          <Button type="submit" disabled={busy || !email} className="shrink-0">
            <PlusIcon size={13} />
            Add member
          </Button>
        </form>
      )}
      {error && (
        <p role="alert" className="-mt-2 text-xs text-danger">
          {error}
        </p>
      )}

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
                    <Badge tone={roleTone[m.role]}>{m.role}</Badge>
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
                  {canRemove(m) && (
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
