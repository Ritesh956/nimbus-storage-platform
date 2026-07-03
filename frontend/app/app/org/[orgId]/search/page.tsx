"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
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
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <h1 className="text-xl font-semibold">Search</h1>
      <form onSubmit={search} className="flex gap-2">
        <Input placeholder="File name…" value={q} onChange={(e) => setQ(e.target.value)} />
        <Input placeholder="Type, e.g. image" value={type} onChange={(e) => setType(e.target.value)} className="max-w-40" />
        <Button type="submit" disabled={loading}>
          {loading ? "Searching…" : "Search"}
        </Button>
      </form>
      {error && <p className="text-sm text-danger">{error}</p>}

      {results && (
        <div className="flex flex-col gap-2">
          {results.length === 0 && <p className="text-sm text-muted">No results.</p>}
          {results.map((r) => (
            <Link
              key={r.file_id}
              href={`/app/org/${orgId}/folder/${r.folder_id}`}
              className="flex items-center justify-between rounded-lg border border-border px-4 py-3 text-sm hover:border-border-strong hover:bg-surface-2"
            >
              <span>📄 {r.name}</span>
              <span className="text-xs text-muted">
                {formatBytes(r.size_bytes)} · {r.mime_type ?? "unknown"} · {formatDate(r.created_at)}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
