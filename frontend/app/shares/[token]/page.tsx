"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Card, EyebrowLabel } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { LogoMark, FileIcon, DownloadIcon } from "@/components/ui/Icons";
import { formatBytes } from "@/lib/format";
import type { ResolvedShare } from "@/lib/types";

// Deliberately outside app/app/ — this route is never wrapped in
// RequireAuth. That's the entire point of a share link
// (docs/06-api-design.md §7: GET /v1/shares/{token} is public).
export default function SharePage() {
  const { token } = useParams<{ token: string }>();
  const [resolved, setResolved] = useState<ResolvedShare | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [downloading, setDownloading] = useState(false);

  useEffect(() => {
    api.shares
      .resolve(token)
      .then(setResolved)
      .catch((err) => setError(err instanceof ApiError ? err.message : "this link is invalid or has expired"));
  }, [token]);

  async function download() {
    if (!resolved) return;
    setDownloading(true);
    try {
      const parts: BlobPart[] = [];
      for (const chunk of [...resolved.download_plan.chunks].sort((a, b) => a.sequence - b.sequence)) {
        let ok = false;
        for (const url of chunk.targets) {
          const res = await fetch(url);
          if (res.ok) {
            parts.push(await res.blob());
            ok = true;
            break;
          }
        }
        if (!ok) throw new Error(`could not fetch chunk ${chunk.sequence}`);
      }
      const blobUrl = URL.createObjectURL(new Blob(parts));
      const a = document.createElement("a");
      a.href = blobUrl;
      a.download = resolved.file.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(blobUrl);
    } catch (err) {
      setError(err instanceof Error ? err.message : "download failed");
    } finally {
      setDownloading(false);
    }
  }

  return (
    <div className="flex flex-1 items-center justify-center px-4">
      <div className="w-full max-w-md">
        <div className="mb-6 flex flex-col items-center gap-3">
          <LogoMark size={44} />
          <EyebrowLabel>Shared via Nimbus</EyebrowLabel>
        </div>
        <Card className="p-8 text-center">
          {error && <p className="text-sm text-danger">{error}</p>}
          {!error && !resolved && <p className="text-sm text-muted">Loading…</p>}
          {resolved && (
            <>
              <span className="mx-auto grid size-14 place-items-center rounded-2xl bg-surface-deep text-accent">
                <FileIcon size={26} />
              </span>
              <h1 className="mt-4 break-all text-lg font-semibold">{resolved.file.name}</h1>
              <p className="mt-1 text-xs text-muted-2">
                {formatBytes(resolved.file.size_bytes)} · {resolved.file.mime_type}
              </p>
              <Button className="mt-6 w-full py-2.5 text-sm" disabled={downloading} onClick={download}>
                <DownloadIcon size={15} />
                {downloading ? "Downloading…" : "Download"}
              </Button>
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
