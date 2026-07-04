"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { api, ApiError } from "@/lib/api";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { PageHeader } from "@/components/ui/PageHeader";
import { FileIcon, SearchIcon, ChevronDownIcon } from "@/components/ui/Icons";
import { formatBytes, formatDate } from "@/lib/format";
import type { SearchResult } from "@/lib/types";

interface FilterState {
  q: string;
  type: string;
  owner: string; // user_id, "" = anyone
  dateFrom: string; // yyyy-mm-dd from <input type="date">, "" = unset
  dateTo: string;
  sizeMinMB: string;
  sizeMaxMB: string;
}

const emptyFilters: FilterState = { q: "", type: "", owner: "", dateFrom: "", dateTo: "", sizeMinMB: "", sizeMaxMB: "" };

// toParams maps the form's units to the API's: dates widen to full-day
// RFC3339 bounds, sizes go MB → bytes (docs/06-api-design.md §8).
function toParams(f: FilterState): Record<string, string> {
  const p: Record<string, string> = {};
  if (f.q) p.q = f.q;
  if (f.type) p.type = f.type;
  if (f.owner) p.owner = f.owner;
  if (f.dateFrom) p.date_from = `${f.dateFrom}T00:00:00Z`;
  if (f.dateTo) p.date_to = `${f.dateTo}T23:59:59Z`;
  if (f.sizeMinMB) p.size_min = String(Math.floor(Number(f.sizeMinMB) * 1024 * 1024));
  if (f.sizeMaxMB) p.size_max = String(Math.ceil(Number(f.sizeMaxMB) * 1024 * 1024));
  return p;
}

const selectClass =
  "glow-ring w-full rounded-lg border border-border bg-surface-deep px-3 py-2 text-sm text-foreground focus:border-accent";

export default function SearchPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const { data: members } = useSWR(["members", orgId], () => api.orgs.listMembers(orgId));

  const [filters, setFilters] = useState<FilterState>(emptyFilters);
  const [showFilters, setShowFilters] = useState(false);
  const [results, setResults] = useState<SearchResult[] | null>(null);
  // Params of the search the current result list came from — "Load more"
  // must page through *those*, not whatever the form now says.
  const [activeParams, setActiveParams] = useState<Record<string, string>>({});
  const [nextCursor, setNextCursor] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function runSearch(f: FilterState) {
    setLoading(true);
    setError(null);
    try {
      const params = toParams(f);
      const res = await api.orgs.search(orgId, params);
      setResults(res.results);
      setActiveParams(params);
      setNextCursor(res.next_cursor);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "search failed");
    } finally {
      setLoading(false);
    }
  }

  async function loadMore() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.orgs.search(orgId, { ...activeParams, cursor: nextCursor });
      setResults((prev) => [...(prev ?? []), ...res.results]);
      setNextCursor(res.next_cursor);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "search failed");
    } finally {
      setLoading(false);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    void runSearch(filters);
  }

  function clearFilter(patch: Partial<FilterState>) {
    const next = { ...filters, ...patch };
    setFilters(next);
    void runSearch(next);
  }

  const ownerEmail = (id: string) => members?.find((m) => m.user_id === id)?.email ?? id;
  const chips: { label: string; clear: Partial<FilterState> }[] = [
    ...(filters.type ? [{ label: `type: ${filters.type}`, clear: { type: "" } }] : []),
    ...(filters.owner ? [{ label: `owner: ${ownerEmail(filters.owner)}`, clear: { owner: "" } }] : []),
    ...(filters.dateFrom ? [{ label: `from: ${filters.dateFrom}`, clear: { dateFrom: "" } }] : []),
    ...(filters.dateTo ? [{ label: `to: ${filters.dateTo}`, clear: { dateTo: "" } }] : []),
    ...(filters.sizeMinMB ? [{ label: `≥ ${filters.sizeMinMB} MB`, clear: { sizeMinMB: "" } }] : []),
    ...(filters.sizeMaxMB ? [{ label: `≤ ${filters.sizeMaxMB} MB`, clear: { sizeMaxMB: "" } }] : []),
  ];

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Search"
        description="Full-text search across every file name in this organization, with type, owner, date, and size filters."
      />

      <form onSubmit={submit} className="flex flex-col gap-2">
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <SearchIcon size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-2" />
            <Input
              placeholder="Search files…"
              value={filters.q}
              onChange={(e) => setFilters({ ...filters, q: e.target.value })}
              className="pl-9"
            />
          </div>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="secondary"
              onClick={() => setShowFilters((s) => !s)}
              className="shrink-0"
            >
              Filters
              {chips.length > 0 && (
                <span className="rounded bg-accent-soft px-1.5 text-[11px] font-semibold text-accent">
                  {chips.length}
                </span>
              )}
              <ChevronDownIcon size={13} className={`transition-transform ${showFilters ? "rotate-180" : ""}`} />
            </Button>
            <Button type="submit" disabled={loading} className="shrink-0">
              {loading ? "Searching…" : "Search"}
            </Button>
          </div>
        </div>

        {showFilters && (
          <div className="panel grid grid-cols-1 gap-3 p-4 sm:grid-cols-2 lg:grid-cols-3">
            <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
              Type
              <Input
                placeholder="e.g. image, pdf"
                value={filters.type}
                onChange={(e) => setFilters({ ...filters, type: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
              Owner
              <select
                value={filters.owner}
                onChange={(e) => setFilters({ ...filters, owner: e.target.value })}
                className={selectClass}
              >
                <option value="">Anyone</option>
                {members?.map((m) => (
                  <option key={m.user_id} value={m.user_id}>
                    {m.email}
                  </option>
                ))}
              </select>
            </label>
            <div className="grid grid-cols-2 gap-2">
              <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
                From
                <Input
                  type="date"
                  value={filters.dateFrom}
                  onChange={(e) => setFilters({ ...filters, dateFrom: e.target.value })}
                />
              </label>
              <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
                To
                <Input
                  type="date"
                  value={filters.dateTo}
                  onChange={(e) => setFilters({ ...filters, dateTo: e.target.value })}
                />
              </label>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
                Min size (MB)
                <Input
                  type="number"
                  min="0"
                  step="any"
                  value={filters.sizeMinMB}
                  onChange={(e) => setFilters({ ...filters, sizeMinMB: e.target.value })}
                />
              </label>
              <label className="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-muted-2">
                Max size (MB)
                <Input
                  type="number"
                  min="0"
                  step="any"
                  value={filters.sizeMaxMB}
                  onChange={(e) => setFilters({ ...filters, sizeMaxMB: e.target.value })}
                />
              </label>
            </div>
          </div>
        )}

        {chips.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            {chips.map((c) => (
              <button
                key={c.label}
                type="button"
                onClick={() => clearFilter(c.clear)}
                title="Remove filter"
                className="glow-ring inline-flex items-center gap-1.5 rounded border border-accent/25 bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent transition-colors hover:border-accent/50"
              >
                {c.label}
                <span aria-hidden>×</span>
              </button>
            ))}
          </div>
        )}
      </form>
      {error && <p className="text-xs text-danger">{error}</p>}

      {results && (
        <div className="panel overflow-hidden">
          <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">
            Results <span className="ml-1 text-xs font-normal text-muted-2">{results.length}{nextCursor ? "+" : ""}</span>
          </div>
          {results.length === 0 ? (
            <p className="px-5 py-6 text-center text-xs text-muted-2">No results.</p>
          ) : (
            <ul>
              {results.map((r) => (
                <li key={r.file_id} className="border-t border-border/40 first:border-t-0">
                  <Link
                    href={`/app/org/${orgId}/folder/${r.folder_id}`}
                    className="glow-ring flex items-center gap-3 px-5 py-3 transition-colors hover:bg-surface-2/60"
                  >
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent">
                      <FileIcon size={15} />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm">{r.name}</span>
                      <span className="block truncate text-[11px] text-muted-2 sm:hidden">
                        {formatBytes(r.size_bytes)} · {r.mime_type ?? "unknown"}
                      </span>
                    </span>
                    <span className="hidden shrink-0 text-xs text-muted-2 sm:block">
                      {formatBytes(r.size_bytes)} · {r.mime_type ?? "unknown"} · {formatDate(r.created_at)}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
          {nextCursor && results.length > 0 && (
            <div className="border-t border-border/40 p-3 text-center">
              <Button variant="secondary" disabled={loading} onClick={loadMore}>
                {loading ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
