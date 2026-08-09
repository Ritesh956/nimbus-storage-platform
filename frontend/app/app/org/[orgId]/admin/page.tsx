"use client";

import { useState } from "react";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { useLiveEvents } from "@/lib/live";
import { RingDiagram } from "@/components/RingDiagram";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { StatCard } from "@/components/ui/Card";
import { PageHeader } from "@/components/ui/PageHeader";
import { TablePanel, Th, Td, Tr } from "@/components/ui/Table";
import { ServerIcon, PulseIcon, RestoreIcon } from "@/components/ui/Icons";
import { timeAgo } from "@/lib/format";

export default function AdminPage() {
  const { orgId } = useParams<{ orgId: string }>();
  // Cluster ops is platform-admin gated server-side; check /me first so a
  // non-admin who navigates here directly gets a readable explanation
  // instead of silently-empty panels (and we don't fire requests that can
  // only 403).
  const { data: me } = useSWR("me", () => api.auth.me(), { shouldRetryOnError: false });
  const isAdmin = me?.is_platform_admin === true;
  // Health transitions arrive over SSE (backlog #12) — that's what flips a
  // node red the moment the failover demo kills it. The slow poll remains
  // as a fallback and keeps the heartbeat column fresh; SSE only fires on
  // status *changes*.
  const { data: nodes, mutate: mutateNodes } = useSWR(isAdmin ? "admin-nodes" : null, () => api.admin.nodes(), {
    refreshInterval: 10000,
  });
  const { data: dlq, mutate: mutateDlq } = useSWR(isAdmin ? "admin-dlq" : null, () => api.admin.dlq(), {
    refreshInterval: 5000,
  });
  const [dlqError, setDlqError] = useState<string | null>(null);
  useLiveEvents(orgId, { onNodeHealth: () => void mutateNodes() });

  if (me && !isAdmin) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader title="Cluster operations" description="Deployment-wide internals: storage node health, the hash ring, and the event dead-letter queue." />
        <div className="panel flex flex-col items-center gap-2 px-5 py-10 text-center">
          <span className="grid size-10 place-items-center rounded-xl bg-surface-deep text-muted-2">
            <ServerIcon size={18} />
          </span>
          <p className="text-sm text-muted">Platform admin access required.</p>
          <p className="max-w-sm text-xs leading-relaxed text-muted-2">
            This page shows deployment-wide internals (storage nodes, hash ring, dead-letter queue), not
            organization data — access is limited to the single seeded platform-admin account.
          </p>
        </div>
      </div>
    );
  }

  async function retry(id: string) {
    setDlqError(null);
    try {
      await api.admin.retryDeadEvent(id);
      await mutateDlq();
    } catch (err) {
      setDlqError(err instanceof ApiError ? err.message : "retry failed");
    }
  }

  const healthy = nodes?.filter((n) => n.status === "healthy").length ?? 0;
  const down = (nodes?.length ?? 0) - healthy;

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Cluster operations"
        description="Consistent-hash-routed storage nodes (health-checked every 2s, changes stream in live), the hash ring, and the event dead-letter queue."
      />

      {nodes && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <StatCard
            label="Total nodes"
            value={nodes.length}
            icon={<ServerIcon size={14} />}
          />
          <StatCard
            label="Healthy"
            value={healthy}
            icon={<PulseIcon size={14} />}
            chip={nodes.length ? `${Math.round((healthy / nodes.length) * 100)}%` : undefined}
            chipTone="success"
          />
          <StatCard
            label="Down"
            value={down}
            icon={<ServerIcon size={14} />}
            chip={down > 0 ? "failover active" : undefined}
            chipTone={down > 0 ? "danger" : "neutral"}
          />
        </div>
      )}

      <TablePanel title="Nodes">
        <thead>
          <tr>
            <Th>Node</Th>
            <Th className="hidden md:table-cell">Endpoint</Th>
            <Th>Status</Th>
            <Th className="hidden text-right sm:table-cell">Last heartbeat</Th>
          </tr>
        </thead>
        <tbody>
          {nodes?.map((n) => (
            <Tr key={n.id}>
              <Td className="font-medium">
                <span className="flex items-center gap-2.5">
                  <span
                    className={`grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep ${
                      n.status === "healthy" ? "text-accent-2" : "text-danger"
                    }`}
                  >
                    <ServerIcon size={15} />
                  </span>
                  {n.id}
                </span>
              </Td>
              <Td className="hidden font-mono text-xs text-muted-2 md:table-cell">{n.endpoint}</Td>
              <Td>
                <Badge tone={n.status === "healthy" ? "success" : "danger"}>{n.status}</Badge>
              </Td>
              <Td className="hidden text-right text-xs text-muted-2 sm:table-cell">
                {n.last_heartbeat_at ? timeAgo(n.last_heartbeat_at) : "never seen"}
              </Td>
            </Tr>
          ))}
        </tbody>
      </TablePanel>

      <RingDiagram orgId={orgId} nodes={nodes} />

      <TablePanel title="Dead-letter queue">
        <thead>
          <tr>
            <Th>Event</Th>
            <Th className="hidden md:table-cell">Error</Th>
            <Th>Status</Th>
            <Th className="text-right">Actions</Th>
          </tr>
        </thead>
        <tbody>
          {dlq?.events.map((e) => (
            <Tr key={e.id}>
              <Td>
                <span className="block font-mono text-xs">{e.subject}</span>
                <span className="block text-[11px] text-muted-2">
                  {timeAgo(e.created_at)} · {e.deliveries} deliveries
                </span>
              </Td>
              <Td className="hidden max-w-64 md:table-cell">
                <span className="block truncate text-xs text-muted" title={e.error}>
                  {e.error}
                </span>
              </Td>
              <Td>
                <Badge tone={e.status === "dead" ? "danger" : "success"}>{e.status}</Badge>
              </Td>
              <Td className="text-right">
                {e.status === "dead" && (
                  <Button variant="secondary" onClick={() => retry(e.id)}>
                    <RestoreIcon size={12} />
                    Retry
                  </Button>
                )}
              </Td>
            </Tr>
          ))}
          {dlq && dlq.events.length === 0 && (
            <Tr>
              <Td className="text-xs text-muted-2">No dead events — every delivery has succeeded or is still retrying.</Td>
              <Td className="hidden md:table-cell" />
              <Td />
              <Td />
            </Tr>
          )}
        </tbody>
      </TablePanel>
      {dlqError && (
        <p role="alert" className="-mt-3 text-xs text-danger">
          {dlqError}
        </p>
      )}
    </div>
  );
}
