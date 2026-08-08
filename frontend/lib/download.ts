import type { DownloadPlan } from "./types";

async function sha256Hex(data: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

// assembleFromPlan reassembles a file client-side from a download plan's
// presigned chunk URLs (docs/06-api-design.md §6): primary replica first,
// falling back through the remaining targets in order on failure. Each
// fetched chunk is re-hashed against the content hash the plan already
// carries and rejected on mismatch before being accepted — the download path
// has no server-side integrity check (the server never touches these
// bytes), so without this a single corrupted replica returning a 200 with
// bad bytes would be served straight through with nothing to catch it,
// unlike the worker's own chunk-reassembly path (§03 audit gap).
export async function assembleFromPlan(plan: DownloadPlan): Promise<Blob> {
  const parts: BlobPart[] = [];
  for (const chunk of [...plan.chunks].sort((a, b) => a.sequence - b.sequence)) {
    let verified: ArrayBuffer | null = null;
    for (const url of chunk.targets) {
      const res = await fetch(url);
      if (!res.ok) continue;
      const buf = await res.arrayBuffer();
      if ((await sha256Hex(buf)) === chunk.hash) {
        verified = buf;
        break;
      }
      // Wrong bytes from this replica — try the next one rather than
      // trusting it just because the HTTP request itself succeeded.
    }
    if (!verified) throw new Error(`could not fetch a valid copy of chunk ${chunk.sequence} from any replica`);
    parts.push(verified);
  }
  return new Blob(parts);
}

export function saveBlob(blob: Blob, name: string) {
  const blobUrl = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = blobUrl;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(blobUrl);
}
