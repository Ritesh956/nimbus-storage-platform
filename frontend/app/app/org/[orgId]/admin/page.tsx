"use client";

import useSWR from "swr";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { timeAgo } from "@/lib/format";

export default function AdminPage() {
  // Polling, not a one-shot fetch — this page is meant to be on screen
  // during a live failover demo (docs/07-distributed-architecture.md §5),
  // where a node's status changes without any user action to trigger a refetch.
  const { data: nodes } = useSWR("admin-nodes", () => api.admin.nodes(), { refreshInterval: 2000 });

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4">
      <h1 className="text-xl font-semibold">Storage nodes</h1>
      <p className="text-sm text-muted">
        Consistent-hash-routed storage nodes, health-checked every 2s. Refreshes automatically.
      </p>
      <div className="flex flex-col gap-2">
        {nodes?.map((n) => (
          <Card key={n.id} className="flex items-center justify-between">
            <div>
              <div className="font-medium">{n.id}</div>
              <div className="text-xs text-muted">{n.endpoint}</div>
            </div>
            <div className="text-right">
              <Badge tone={n.status === "healthy" ? "success" : "danger"}>{n.status}</Badge>
              <div className="mt-1 text-xs text-muted">
                {n.last_heartbeat_at ? `heartbeat ${timeAgo(n.last_heartbeat_at)}` : "never seen"}
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
