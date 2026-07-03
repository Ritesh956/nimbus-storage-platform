"use client";

import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";

// Each label reads directly into "{label} {target_type} {id}" below
// (e.g. "uploaded a" + "file" + "ed5b0647") — found a redundant
// "uploaded a file file ed5b0647" by actually looking at the rendered
// page, not just the code.
const verbLabels: Record<string, string> = {
  uploaded: "uploaded a",
  thumbnail_generated: "generated a thumbnail for the",
};

export default function ActivityPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data } = useSWR(["activity", orgId], () => api.orgs.activity(orgId));

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4">
      <h1 className="text-xl font-semibold">Activity</h1>
      {data?.events.length === 0 && <p className="text-sm text-muted">No activity yet.</p>}
      <ul className="flex flex-col gap-2">
        {data?.events.map((e, i) => (
          <li key={i} className="flex items-center justify-between rounded-lg border border-border px-4 py-3 text-sm">
            <span>
              <span className="text-accent">{e.actor ? "A member" : "nimbus-worker"}</span>{" "}
              {verbLabels[e.verb] ?? e.verb} {e.target_type} <code className="text-xs text-muted">{e.target_id.slice(0, 8)}</code>
            </span>
            <span className="text-xs text-muted">{timeAgo(e.created_at)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
