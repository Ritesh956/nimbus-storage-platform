"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { PageHeader } from "@/components/ui/PageHeader";
import { FileIcon, SearchIcon } from "@/components/ui/Icons";
import { formatBytes, formatDate } from "@/lib/format";
import type { SearchResult } from "@/lib/types";

export default function SearchPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const [q, setQ] = useState("");
  const [type, setType] = useState("");
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function search(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const params: Record<string, string> = {};
      if (q) params.q = q;
      if (type) params.type = type;
      const res = await api.orgs.search(orgId, params);
      setResults(res.results);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "search failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Search" description="Full-text search across every file name in this organization." />

      <form onSubmit={search} className="flex gap-2">
        <div className="relative flex-1">
          <SearchIcon size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-2" />
          <Input placeholder="Search files…" value={q} onChange={(e) => setQ(e.target.value)} className="pl-9" />
        </div>
        <Input
          placeholder="Type, e.g. image"
          value={type}
          onChange={(e) => setType(e.target.value)}
          className="max-w-36"
        />
        <Button type="submit" disabled={loading} className="shrink-0">
          {loading ? "Searching…" : "Search"}
        </Button>
      </form>
      {error && <p className="text-xs text-danger">{error}</p>}

      {results && (
        <div className="panel overflow-hidden">
          <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">
            Results <span className="ml-1 text-xs font-normal text-muted-2">{results.length}</span>
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
                    <span className="flex-1 truncate text-sm">{r.name}</span>
                    <span className="shrink-0 text-xs text-muted-2">
                      {formatBytes(r.size_bytes)} · {r.mime_type ?? "unknown"} · {formatDate(r.created_at)}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
