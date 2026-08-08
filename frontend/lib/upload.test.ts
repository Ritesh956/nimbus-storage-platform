import { describe, it, expect, vi, beforeEach } from "vitest";
import { ApiError } from "./api";

// Audit §14/next-session.md: the upload resume/fingerprint logic in
// lib/upload.ts was explicitly named as untested intricate client logic.
// fingerprintFor/loadResume/saveResume/clearResume are module-private, so
// this tests them through uploadFile's observable behavior instead —
// which localStorage key state results, and which api.uploads.* calls
// happen — rather than reaching into implementation details. api.ts and
// crypto.subtle are both mocked: this is a unit test of the resume/
// fingerprint state machine, not an integration test of real chunking
// (that's covered by the backend's own upload integration tests plus
// scripts/smoke-upload.js).

const initMock = vi.fn();
const checkChunksMock = vi.fn();
const initChunkMock = vi.fn();
const commitChunkMock = vi.fn();
const completeMock = vi.fn();

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ApiError: actual.ApiError,
    api: {
      uploads: {
        init: (...args: unknown[]) => initMock(...args),
        checkChunks: (...args: unknown[]) => checkChunksMock(...args),
        initChunk: (...args: unknown[]) => initChunkMock(...args),
        commitChunk: (...args: unknown[]) => commitChunkMock(...args),
        complete: (...args: unknown[]) => completeMock(...args),
      },
    },
  };
});

// jsdom's fetch isn't polyfilled by default — only used here for the
// direct chunk PUT to a presigned URL, not for api.* calls (those are
// mocked above).
const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

// jsdom's SubtleCrypto rejects the ArrayBuffer File.slice().arrayBuffer()
// produces with "2nd argument is not instance of ArrayBuffer" — a
// cross-realm instanceof mismatch between jsdom's Blob polyfill and its
// crypto implementation, not a real browser behavior (caught by an actual
// test run, not by reasoning about the code). Stubbed here rather than
// worked around in app code: this suite tests the resume/fingerprint state
// machine, not per-chunk hash correctness, which Sha256Stream's own test
// suite already verifies against Node's real crypto as an oracle.
vi.stubGlobal("crypto", {
  ...globalThis.crypto,
  subtle: {
    digest: async () => new ArrayBuffer(32),
  },
});

function makeFile(bytes: number, name = "test.bin"): File {
  const data = new Uint8Array(bytes).fill(7);
  return new File([data], name, { type: "application/octet-stream", lastModified: 1_700_000_000_000 });
}

const noopProgress = () => {};

beforeEach(() => {
  window.localStorage.clear();
  initMock.mockReset();
  checkChunksMock.mockReset();
  initChunkMock.mockReset();
  commitChunkMock.mockReset();
  completeMock.mockReset();
  fetchMock.mockReset();

  initMock.mockResolvedValue({ upload_id: "upload-1" });
  checkChunksMock.mockResolvedValue({ missing: [] }); // small test files dedupe trivially — no chunk PUTs needed
  completeMock.mockResolvedValue({ file_id: "file-1", version_id: "version-1" });
});

describe("uploadFile resume/fingerprint behavior", () => {
  it("calls init on a fresh upload and clears any resume record on success", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(1024);

    const result = await uploadFile(file, { folderId: "folder-1" }, noopProgress);

    expect(result).toEqual({ file_id: "file-1", version_id: "version-1" });
    expect(initMock).toHaveBeenCalledTimes(1);
    expect(initMock).toHaveBeenCalledWith(
      expect.objectContaining({ folderId: "folder-1", name: "test.bin", sizeBytes: 1024 }),
    );
    expect(completeMock).toHaveBeenCalledWith("upload-1", expect.any(Array), 1024, expect.any(String));

    // A successful upload must not leave a resume record behind — the next
    // attempt for the same file+target should start clean, not think a
    // finished upload is still "in progress".
    const keys = Object.keys(window.localStorage).filter((k) => k.startsWith("nimbus_upload_resume:"));
    expect(keys).toHaveLength(0);
  });

  it("reuses a matching resume record instead of calling init again", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(2048, "resume-me.bin");

    // Fingerprint must match fingerprintFor's own composition exactly:
    // [name, size, lastModified, folderId, fileId].join("|").
    const fingerprint = ["resume-me.bin", 2048, 1_700_000_000_000, "folder-1", ""].join("|");
    window.localStorage.setItem(
      "nimbus_upload_resume:" + fingerprint,
      JSON.stringify({ uploadId: "resumed-upload-id", totalBytes: 2048, savedAt: Date.now() }),
    );

    const result = await uploadFile(file, { folderId: "folder-1" }, noopProgress);

    expect(result).toEqual({ file_id: "file-1", version_id: "version-1" });
    expect(initMock).not.toHaveBeenCalled();
    expect(checkChunksMock).toHaveBeenCalledWith("resumed-upload-id", expect.any(Array));
    expect(completeMock).toHaveBeenCalledWith("resumed-upload-id", expect.any(Array), 2048, expect.any(String));
  });

  it("ignores a resume record whose size no longer matches the file (starts fresh instead)", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(4096, "changed.bin");

    const fingerprint = ["changed.bin", 4096, 1_700_000_000_000, "folder-1", ""].join("|");
    window.localStorage.setItem(
      "nimbus_upload_resume:" + fingerprint,
      JSON.stringify({ uploadId: "stale-upload-id", totalBytes: 999, savedAt: Date.now() }), // size mismatch
    );

    await uploadFile(file, { folderId: "folder-1" }, noopProgress);

    expect(initMock).toHaveBeenCalledTimes(1);
    expect(checkChunksMock).not.toHaveBeenCalledWith("stale-upload-id", expect.any(Array));
  });

  it("scopes resume records to the target (a different folderId does not reuse another target's record)", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(512, "same-name.bin");

    const fingerprintForOtherFolder = ["same-name.bin", 512, 1_700_000_000_000, "folder-OTHER", ""].join("|");
    window.localStorage.setItem(
      "nimbus_upload_resume:" + fingerprintForOtherFolder,
      JSON.stringify({ uploadId: "other-folder-upload", totalBytes: 512, savedAt: Date.now() }),
    );

    await uploadFile(file, { folderId: "folder-1" }, noopProgress);

    // Uploading into folder-1 must not pick up a record saved under folder-OTHER.
    expect(initMock).toHaveBeenCalledTimes(1);
    expect(checkChunksMock).not.toHaveBeenCalledWith("other-folder-upload", expect.any(Array));
  });

  it("drops the resume record when the server reports the upload is gone (404/409), rather than retrying it forever", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(256, "dead-upload.bin");

    checkChunksMock.mockRejectedValueOnce(new ApiError(404, "not_found", "upload not found"));

    await expect(uploadFile(file, { folderId: "folder-1" }, noopProgress)).rejects.toThrow();

    const keys = Object.keys(window.localStorage).filter((k) => k.startsWith("nimbus_upload_resume:"));
    expect(keys).toHaveLength(0);
  });

  it("keeps the resume record on a transient error, so a retry can pick up where it left off", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(256, "flaky-network.bin");

    checkChunksMock.mockRejectedValueOnce(new Error("network blip"));

    await expect(uploadFile(file, { folderId: "folder-1" }, noopProgress)).rejects.toThrow("network blip");

    const keys = Object.keys(window.localStorage).filter((k) => k.startsWith("nimbus_upload_resume:"));
    expect(keys).toHaveLength(1); // init already ran and saved a resume record before checkChunks failed
  });

  it("reports progress through the hashing -> uploading -> completing -> done sequence", async () => {
    const { uploadFile } = await import("./upload");
    const file = makeFile(100);
    const statuses: string[] = [];

    await uploadFile(file, { folderId: "folder-1" }, (p) => statuses.push(p.status));

    expect(statuses[0]).toBe("hashing");
    expect(statuses).toContain("uploading");
    expect(statuses).toContain("completing");
    expect(statuses[statuses.length - 1]).toBe("done");
  });
});
