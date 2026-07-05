"use client";

import { useParams } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { useLiveEvents } from "@/lib/live";
import { timeAgo } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { ClockIcon } from "@/components/ui/Icons";

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
  const { data, mutate } = useSWR(["activity", orgId], () => api.orgs.activity(orgId));
  // New events (own uploads, other members', the worker's thumbnails) push
  // over SSE — each one just revalidates the feed (backlog #12).
  useLiveEvents(orgId, { onActivity: () => void mutate() });

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Activity"
        description="Everything that happened in this organization, newest first."
      />

      <div className="panel overflow-hidden">
        {data?.events.length === 0 && (
          <p className="px-5 py-6 text-center text-xs text-muted-2">No activity yet.</p>
        )}
        <ul>
          {data?.events.map((e, i) => {
            const isWorker = !e.actor;
            return (
              <li
                key={i}
                className="flex items-center gap-3 border-t border-border/40 px-4 py-3 text-sm first:border-t-0 hover:bg-surface-2/60 sm:px-5"
              >
                <span
                  className={`grid size-8 shrink-0 place-items-center rounded-full text-[10px] font-bold text-white ${
                    isWorker ? "bg-accent-2/80" : "gradient-primary"
                  }`}
                >
                  {isWorker ? "W" : "M"}
                </span>
                <span className="min-w-0 flex-1 truncate">
                  <span className={isWorker ? "font-medium text-accent-2" : "font-medium text-accent"}>
                    {isWorker ? "nimbus-worker" : "A member"}
                  </span>{" "}
                  <span className="text-muted">
                    {verbLabels[e.verb] ?? e.verb} {e.target_type}
                  </span>{" "}
                  <code className="rounded bg-surface-deep px-1.5 py-0.5 font-mono text-[11px] text-muted-2">
                    {e.target_id.slice(0, 8)}
                  </code>
                </span>
                <span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-2">
                  <ClockIcon size={12} />
                  {timeAgo(e.created_at)}
                </span>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
