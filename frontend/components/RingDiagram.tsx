"use client";

import { FormEvent, useMemo, useState } from "react";
import useSWR from "swr";
import { api } from "@/lib/api";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Badge } from "./ui/Badge";
import type { RingChunk, SearchResult, StorageNode } from "@/lib/types";

// Stable per-node colors from the Dashdark palette; cycles if the cluster
// ever grows past three nodes.
const NODE_COLORS = ["var(--accent)", "var(--accent-2)", "var(--warning)"];

const TAU = Math.PI * 2;
const RING_MAX = 2 ** 32;

function angleOf(position: number): number {
  return (position / RING_MAX) * TAU - Math.PI / 2; // 0 at 12 o'clock, clockwise
}

function polar(cx: number, cy: number, r: number, angle: number): [number, number] {
  return [cx + r * Math.cos(angle), cy + r * Math.sin(angle)];
}

// RingDiagram renders the consistent-hash ring behind chunk placement
// (backlog #13): every vnode as a tick at its actual uint32 position, and —
// once a file is looked up — that file's chunks as markers where its hashes
// land, alongside the recorded replica locations. The vnode layout comes
// from GET /v1/admin/ring (the same ring Resolve walks, not a re-derivation);
// node health colors come from the caller's /admin/nodes data so the ring
// dims live with the rest of the admin page when a node dies.
export function RingDiagram({ orgId, nodes }: { orgId: string; nodes: StorageNode[] | undefined }) {
  const { data: ring } = useSWR("admin-ring", () => api.admin.ring());
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [selected, setSelected] = useState<{ id: string; name: string } | null>(null);
  const { data: fileRing } = useSWR(selected ? ["ring-file", selected.id] : null, () =>
    api.admin.ring(selected!.id),
  );

  const nodeIds = useMemo(() => {
    const ids = [...new Set((ring?.vnodes ?? []).map((v) => v.node))];
    return ids.sort();
  }, [ring]);
  const colorOf = (node: string) => NODE_COLORS[Math.max(0, nodeIds.indexOf(node)) % NODE_COLORS.length];
  const isDown = (node: string) => nodes?.find((n) => n.id === node)?.status === "down";

  async function search(e: FormEvent) {
    e.preventDefault();
    if (!query.trim()) return;
    const { results } = await api.orgs.search(orgId, { q: query.trim(), limit: "6" });
    setResults(results);
  }

  const chunks: RingChunk[] = fileRing?.chunks ?? [];
  const n = ring?.replication_factor ?? 2;
  const cx = 180, cy = 180, R = 132;

  return (
    <div className="panel overflow-hidden">
      <div className="flex flex-col gap-1 border-b border-border/60 px-4 py-3.5 sm:px-5">
        <span className="text-sm font-medium">Hash ring</span>
        <span className="text-xs text-muted-2">
          {ring ? `${ring.vnodes.length} vnodes across ${nodeIds.length} nodes` : "Loading ring…"} — SHA-1 positions on
          the uint32 ring; chunks are placed on the first {n} distinct healthy nodes walking clockwise from their hash.
        </span>
      </div>

      <div className="flex flex-col gap-5 p-4 sm:p-5 lg:flex-row">
        <svg viewBox="0 0 360 360" className="mx-auto w-full max-w-90 shrink-0" role="img" aria-label="Consistent hash ring">
          <circle cx={cx} cy={cy} r={R} fill="none" stroke="var(--border)" strokeWidth="1" />
          {(ring?.vnodes ?? []).map((v, i) => {
            const a = angleOf(v.position);
            const [x1, y1] = polar(cx, cy, R - 6, a);
            const [x2, y2] = polar(cx, cy, R + 6, a);
            return (
              <line
                key={i}
                x1={x1} y1={y1} x2={x2} y2={y2}
                stroke={colorOf(v.node)}
                strokeWidth="1.4"
                opacity={isDown(v.node) ? 0.15 : 0.75}
              >
                <title>{`${v.node} vnode @ ${((v.position / RING_MAX) * 100).toFixed(1)}%`}</title>
              </line>
            );
          })}
          {chunks.map((c) => {
            const a = angleOf(c.position);
            const [lx1, ly1] = polar(cx, cy, R + 8, a);
            const [lx2, ly2] = polar(cx, cy, R + 22, a);
            const [mx, my] = polar(cx, cy, R + 30, a);
            return (
              <g key={c.hash}>
                <line x1={lx1} y1={ly1} x2={lx2} y2={ly2} stroke="var(--muted-2)" strokeWidth="1" />
                <circle cx={mx} cy={my} r="8.5" fill="var(--surface-deep)" stroke={colorOf(c.locations[0] ?? c.preference[0] ?? "")} strokeWidth="1.5">
                  <title>{`chunk ${c.sequence}: ${c.hash.slice(0, 12)}… → ${c.locations.join(", ") || "no recorded replicas"}`}</title>
                </circle>
                <text x={mx} y={my + 3} textAnchor="middle" fontSize="9" fill="var(--muted)">
                  {c.sequence}
                </text>
              </g>
            );
          })}
          <text x={cx} y={cy - 6} textAnchor="middle" fontSize="12" fill="var(--muted)">
            {selected ? selected.name : "no file selected"}
          </text>
          <text x={cx} y={cy + 12} textAnchor="middle" fontSize="10" fill="var(--muted-2)">
            {selected ? `${chunks.length} chunk${chunks.length === 1 ? "" : "s"}` : "search below to plot chunks"}
          </text>
        </svg>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <div className="flex flex-wrap gap-2">
            {nodeIds.map((id) => (
              <span key={id} className="flex items-center gap-1.5 rounded-lg bg-surface-deep px-2.5 py-1.5 text-xs">
                <span className="size-2 rounded-full" style={{ background: colorOf(id), opacity: isDown(id) ? 0.3 : 1 }} />
                {id}
                {isDown(id) && <Badge tone="danger">down</Badge>}
              </span>
            ))}
          </div>

          <form onSubmit={search} className="flex gap-2">
            <Input
              placeholder="Find a file by name to plot its chunks…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <Button type="submit" variant="secondary" className="shrink-0">
              Locate
            </Button>
          </form>

          {results && !selected && (
            <ul className="flex flex-col gap-1">
              {results.length === 0 && <li className="text-xs text-muted-2">No files match.</li>}
              {results.map((r) => (
                <li key={r.file_id}>
                  <button
                    onClick={() => setSelected({ id: r.file_id, name: r.name })}
                    className="glow-ring w-full rounded-lg bg-surface-deep px-3 py-2 text-left text-xs transition-colors hover:bg-surface-2"
                  >
                    {r.name}
                  </button>
                </li>
              ))}
            </ul>
          )}

          {selected && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <span className="truncate text-xs font-medium">{selected.name}</span>
                <button
                  onClick={() => {
                    setSelected(null);
                    setResults(null);
                    setQuery("");
                  }}
                  className="glow-ring rounded text-xs text-muted-2 transition-colors hover:text-foreground"
                >
                  Clear
                </button>
              </div>
              <ul className="flex flex-col gap-1.5">
                {chunks.map((c) => {
                  // The ring's would-be placement today vs. where the bytes
                  // actually are — they diverge after a failover write.
                  const planned = c.preference.slice(0, n);
                  const failover = c.locations.length > 0 && !planned.every((p) => c.locations.includes(p));
                  return (
                    <li key={c.hash} className="rounded-lg bg-surface-deep px-3 py-2 text-xs">
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-mono text-[11px] text-muted">
                          #{c.sequence} {c.hash.slice(0, 12)}…
                        </span>
                        <span className="text-[11px] text-muted-2">{((c.position / RING_MAX) * 100).toFixed(1)}% around</span>
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-1.5">
                        {c.locations.map((l) => (
                          <span key={l} className="flex items-center gap-1 rounded bg-surface px-1.5 py-0.5 text-[11px]">
                            <span className="size-1.5 rounded-full" style={{ background: colorOf(l) }} />
                            {l}
                          </span>
                        ))}
                        {failover && <Badge tone="warning">failover placement</Badge>}
                      </div>
                    </li>
                  );
                })}
                {fileRing && chunks.length === 0 && (
                  <li className="text-xs text-muted-2">This file has no stored chunks (empty or metadata-only).</li>
                )}
              </ul>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
