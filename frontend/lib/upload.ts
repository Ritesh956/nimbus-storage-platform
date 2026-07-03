import { api } from "./api";

// Matches the backend's default NIMBUS_CHUNK_SIZE_BYTES (docs/02-system-design.md §2.1).
const CHUNK_SIZE = 8 * 1024 * 1024;

export interface UploadProgress {
  fileName: string;
  loadedBytes: number;
  totalBytes: number;
  status: "hashing" | "uploading" | "committing" | "completing" | "done" | "error";
  error?: string;
}

async function sha256Hex(data: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

// uploadFile drives the full chunked/resumable/deduplicated flow
// (docs/06-api-design.md §5) from the browser: hash locally, skip chunks
// the server already has, upload the rest directly to storage nodes via
// presigned URLs, then complete. Reads the whole file into memory once
// (browsers have no incremental SubtleCrypto hash API) — fine at demo
// scale, a documented limit for very large files rather than a true
// streaming implementation.
export async function uploadFile(
  file: File,
  target: { folderId?: string; fileId?: string },
  onProgress: (p: UploadProgress) => void,
): Promise<{ file_id: string; version_id: string }> {
  const report = (p: Partial<UploadProgress>) =>
    onProgress({ fileName: file.name, loadedBytes: 0, totalBytes: file.size, status: "hashing", ...p });

  try {
    const buffer = await file.arrayBuffer();
    const total = buffer.byteLength;

    report({ status: "hashing", loadedBytes: 0, totalBytes: total });
    const wholeHash = await sha256Hex(buffer);

    const chunks: { hash: string; start: number; end: number }[] = [];
    for (let start = 0; start < total; start += CHUNK_SIZE) {
      const end = Math.min(start + CHUNK_SIZE, total);
      chunks.push({ hash: await sha256Hex(buffer.slice(start, end)), start, end });
    }
    if (chunks.length === 0) chunks.push({ hash: wholeHash, start: 0, end: 0 }); // zero-byte file

    const hashes = chunks.map((c) => c.hash);
    const { missing } = await api.uploads.checkChunks(hashes);
    const missingSet = new Set(missing);

    const { upload_id } = await api.uploads.init({
      folderId: target.folderId,
      fileId: target.fileId,
      name: target.fileId ? undefined : file.name,
      sizeBytes: total,
      mimeType: file.type || "application/octet-stream",
    });

    let uploadedBytes = 0;
    report({ status: "uploading", loadedBytes: 0, totalBytes: total });

    for (const chunk of chunks) {
      if (missingSet.has(chunk.hash)) {
        const { targets } = await api.uploads.initChunk(upload_id, chunk.hash);
        const chunkData = buffer.slice(chunk.start, chunk.end);
        const etags: Record<string, string> = {};
        for (const t of targets) {
          const res = await fetch(t.put_url, { method: "PUT", body: chunkData });
          if (!res.ok) throw new Error(`chunk upload to ${t.node_id} failed: ${res.status}`);
          etags[t.node_id] = res.headers.get("etag") ?? "";
        }
        await api.uploads.commitChunk(upload_id, chunk.hash, chunk.end - chunk.start, etags);
      }
      uploadedBytes += chunk.end - chunk.start;
      report({ status: "uploading", loadedBytes: uploadedBytes, totalBytes: total });
    }

    report({ status: "completing", loadedBytes: total, totalBytes: total });
    const result = await api.uploads.complete(upload_id, hashes, total, wholeHash);
    report({ status: "done", loadedBytes: total, totalBytes: total });
    return result;
  } catch (err) {
    report({ status: "error", error: err instanceof Error ? err.message : String(err) });
    throw err;
  }
}
