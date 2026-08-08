import { api, ApiError } from "./api";
import { Sha256Stream } from "./sha256-stream";

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

// Resume-after-reload (§03 audit gap): a page refresh loses uploadFile's
// closed-over JS state, but content-addressed dedup means a fresh init +
// checkChunks pass already skips re-uploading any chunk this file's earlier,
// interrupted attempt managed to commit (that chunk is now globally known).
// The only real loss on a plain retry is a second orphaned `uploads` row and
// re-hashing the file from disk — this record lets a retry of the *same*
// file reuse the same upload_id instead, avoiding the orphan. It does not
// survive picking a different file, and there's no auto-resume UI: the user
// has to re-select/re-drop the file after a reload, since browsers don't
// persist File handles across navigations.
const RESUME_KEY_PREFIX = "nimbus_upload_resume:";
const RESUME_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

interface ResumeRecord {
  uploadId: string;
  totalBytes: number;
  savedAt: number;
}

function fingerprintFor(file: File, target: { folderId?: string; fileId?: string }): string {
  return [file.name, file.size, file.lastModified, target.folderId ?? "", target.fileId ?? ""].join("|");
}

function loadResume(fingerprint: string): ResumeRecord | null {
  try {
    const raw = window.localStorage.getItem(RESUME_KEY_PREFIX + fingerprint);
    if (!raw) return null;
    const rec = JSON.parse(raw) as ResumeRecord;
    if (Date.now() - rec.savedAt > RESUME_MAX_AGE_MS) return null;
    return rec;
  } catch {
    return null;
  }
}

function saveResume(fingerprint: string, rec: ResumeRecord) {
  try {
    window.localStorage.setItem(RESUME_KEY_PREFIX + fingerprint, JSON.stringify(rec));
  } catch {
    // localStorage unavailable/full — resume is a best-effort convenience,
    // not required for the upload itself to succeed.
  }
}

function clearResume(fingerprint: string) {
  try {
    window.localStorage.removeItem(RESUME_KEY_PREFIX + fingerprint);
  } catch {
    // best-effort, see saveResume
  }
}

// uploadFile drives the full chunked/resumable/deduplicated flow
// (docs/06-api-design.md §5) from the browser: hash locally, skip chunks
// the server already has, upload the rest directly to storage nodes via
// presigned URLs, then complete. Reads the file one File.slice() chunk
// (CHUNK_SIZE) at a time rather than one arrayBuffer() call for the whole
// file (§03 audit gap) — peak memory is one chunk, not the whole upload.
// Per-chunk content hashes still use SubtleCrypto (each slice is already
// bounded); the whole-file checksum uses Sha256Stream since SubtleCrypto
// has no incremental digest API.
export async function uploadFile(
  file: File,
  target: { folderId?: string; fileId?: string },
  onProgress: (p: UploadProgress) => void,
): Promise<{ file_id: string; version_id: string }> {
  const report = (p: Partial<UploadProgress>) =>
    onProgress({ fileName: file.name, loadedBytes: 0, totalBytes: file.size, status: "hashing", ...p });
  const fingerprint = fingerprintFor(file, target);

  try {
    const total = file.size;
    report({ status: "hashing", loadedBytes: 0, totalBytes: total });

    const chunks: { hash: string; start: number; end: number }[] = [];
    const wholeHasher = new Sha256Stream();
    let hashedBytes = 0;
    for (let start = 0; start < total; start += CHUNK_SIZE) {
      const end = Math.min(start + CHUNK_SIZE, total);
      const buf = await file.slice(start, end).arrayBuffer();
      wholeHasher.update(new Uint8Array(buf));
      chunks.push({ hash: await sha256Hex(buf), start, end });
      hashedBytes = end;
      report({ status: "hashing", loadedBytes: hashedBytes, totalBytes: total });
    }
    const wholeHash = wholeHasher.digestHex();
    if (chunks.length === 0) chunks.push({ hash: wholeHash, start: 0, end: 0 }); // zero-byte file

    const hashes = chunks.map((c) => c.hash);

    // init (or resume) before checkChunks: the dedup check is upload-scoped
    // now (audit §05 — what does *this org* still need to upload, not "does
    // this content exist anywhere"), so it needs an upload_id to answer.
    const resumed = loadResume(fingerprint);
    let upload_id: string;
    if (resumed && resumed.totalBytes === total) {
      upload_id = resumed.uploadId;
    } else {
      ({ upload_id } = await api.uploads.init({
        folderId: target.folderId,
        fileId: target.fileId,
        name: target.fileId ? undefined : file.name,
        sizeBytes: total,
        mimeType: file.type || "application/octet-stream",
      }));
      saveResume(fingerprint, { uploadId: upload_id, totalBytes: total, savedAt: Date.now() });
    }

    const { missing } = await api.uploads.checkChunks(upload_id, hashes);
    const missingSet = new Set(missing);

    let uploadedBytes = 0;
    report({ status: "uploading", loadedBytes: 0, totalBytes: total });

    for (const chunk of chunks) {
      if (missingSet.has(chunk.hash)) {
        const { targets } = await api.uploads.initChunk(upload_id, chunk.hash);
        const chunkData = await file.slice(chunk.start, chunk.end).arrayBuffer();
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
    clearResume(fingerprint);
    return result;
  } catch (err) {
    // Not-found/not-in-progress means the resumed upload_id is dead (already
    // completed elsewhere, or the record is simply stale) — drop it so the
    // next attempt starts a clean upload instead of retrying a dead one
    // forever. Any other error (network blip, a single chunk PUT failing)
    // keeps the record so a retry of the same file can pick up where it left
    // off.
    if (err instanceof ApiError && (err.status === 404 || err.status === 409)) {
      clearResume(fingerprint);
    }
    report({ status: "error", error: err instanceof Error ? err.message : String(err) });
    throw err;
  }
}
