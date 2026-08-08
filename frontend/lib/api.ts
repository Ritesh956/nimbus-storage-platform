import { getAccessToken, setAccessToken } from "./tokens";
import type {
  ActivityEvent,
  ChunkTarget,
  DeadEvent,
  DownloadPlan,
  FileNode,
  FileSummary,
  FileVersion,
  FolderNode,
  Member,
  Organization,
  OrgUsage,
  ResolvedShare,
  RingInfo,
  SearchResult,
  ShareChildren,
  ShareFileInfo,
  ShareLink,
  StorageNode,
} from "./types";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// localPost calls this Next.js app's own app/api/auth/* route handlers
// (same-origin, so the browser sends the httpOnly refresh cookie
// automatically — no explicit credentials option needed) rather than the Go
// API directly. Those routes are the only place the refresh token is ever
// handled; this client only ever sees the access token they hand back.
async function localPost<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    const err = data?.error ?? {};
    throw new ApiError(res.status, err.code ?? "internal", err.message ?? "request failed");
  }
  return data as T;
}

// Concurrent refresh attempts must not each trigger their own call — that
// races two rotations against the same refresh token, and the loser's reuse
// of an already-rotated token revokes the whole family server-side
// (auth.Repository.RotateRefreshToken), killing the session outright. The
// guard lives inside refresh() itself, not at each call site, specifically
// because AuthProvider's mount-time bootstrap() call and request()'s 401
// handler both call this and don't share a call site — React 18 Strict Mode
// double-invoking that mount effect in dev reproduces the race on every
// single page load otherwise.
let refreshPromise: Promise<void> | null = null;

function refresh(): Promise<void> {
  if (!refreshPromise) {
    refreshPromise = (async () => {
      try {
        const body = await localPost<{ access_token: string }>("/api/auth/refresh");
        setAccessToken(body.access_token);
      } catch (err) {
        setAccessToken(null);
        throw err;
      }
    })().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

async function request<T>(path: string, opts: RequestInit = {}, retry = true): Promise<T> {
  const headers = new Headers(opts.headers);
  const access = getAccessToken();
  if (access) headers.set("Authorization", `Bearer ${access}`);
  if (opts.body && !(opts.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...opts, headers });

  // Unlike the old localStorage-backed refresh token, an httpOnly cookie
  // can't be checked client-side before deciding whether refreshing is
  // worth attempting — a logged-out caller just pays one cheap 401
  // round-trip to /api/auth/refresh instead.
  if (res.status === 401 && retry) {
    try {
      await refresh();
    } catch {
      throw new ApiError(401, "unauthorized", "not logged in");
    }
    return request<T>(path, opts, false);
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) {
    const err = body?.error ?? {};
    throw new ApiError(res.status, err.code ?? "internal", err.message ?? "request failed");
  }
  return body as T;
}

const json = (body: unknown) => JSON.stringify(body);

export const api = {
  auth: {
    register: (email: string, password: string, orgName?: string) =>
      request<{ user_id: string; email: string }>("/v1/auth/register", {
        method: "POST",
        body: json({ email, password, org_name: orgName ?? "" }),
      }),
    // Login is two-step when the account has TOTP enabled: the first call
    // returns a challenge instead of tokens, and totpLogin finishes it.
    // Both go through this app's own /api/auth/* proxy (not request(), which
    // always targets the Go API directly) so the refresh token in the
    // response never touches this module — see app/api/auth/login/route.ts.
    login: async (email: string, password: string): Promise<{ totpRequired: boolean; challengeToken?: string }> => {
      const body = await localPost<{
        access_token?: string;
        totp_required?: boolean;
        challenge_token?: string;
      }>("/api/auth/login", { email, password });
      if (body.totp_required) {
        return { totpRequired: true, challengeToken: body.challenge_token };
      }
      setAccessToken(body.access_token!);
      return { totpRequired: false };
    },
    totpLogin: async (challengeToken: string, code: string) => {
      const body = await localPost<{ access_token: string }>("/api/auth/login/totp", {
        challenge_token: challengeToken,
        code,
      });
      setAccessToken(body.access_token);
    },
    // bootstrap rehydrates an access token from the httpOnly refresh cookie
    // on a cold page load — the only way lib/auth-context.tsx can tell
    // "was I logged in?" now that the refresh token isn't JS-readable.
    bootstrap: () => refresh(),
    forgotPassword: (email: string) =>
      request<void>("/v1/auth/password/forgot", { method: "POST", body: json({ email }) }),
    resetPassword: (token: string, password: string) =>
      request<void>("/v1/auth/password/reset", { method: "POST", body: json({ token, password }) }),
    me: () => request<{ user_id: string; email: string; is_platform_admin: boolean }>("/v1/auth/me"),
    totpStatus: () => request<{ enabled: boolean }>("/v1/auth/totp"),
    totpSetup: () => request<{ secret: string; otpauth_uri: string }>("/v1/auth/totp/setup", { method: "POST" }),
    totpConfirm: (code: string) => request<void>("/v1/auth/totp/confirm", { method: "POST", body: json({ code }) }),
    totpDisable: (code: string) => request<void>("/v1/auth/totp", { method: "DELETE", body: json({ code }) }),
    logout: async () => {
      const access = getAccessToken();
      try {
        await fetch("/api/auth/logout", {
          method: "POST",
          headers: access ? { Authorization: `Bearer ${access}` } : undefined,
        });
      } finally {
        setAccessToken(null);
      }
    },
  },

  orgs: {
    listMine: () => request<Organization[]>("/v1/orgs"),
    create: (name: string) => request<Organization>("/v1/orgs", { method: "POST", body: json({ name }) }),
    listMembers: (orgId: string) => request<Member[]>(`/v1/orgs/${orgId}/members`),
    addMember: (orgId: string, email: string, role: "owner" | "admin" | "member") =>
      request(`/v1/orgs/${orgId}/members`, { method: "POST", body: json({ email, role }) }),
    removeMember: (orgId: string, userId: string) =>
      request(`/v1/orgs/${orgId}/members/${userId}`, { method: "DELETE" }),
    search: (orgId: string, params: Record<string, string>) =>
      request<{ results: SearchResult[]; next_cursor: string }>(
        `/v1/orgs/${orgId}/search?${new URLSearchParams(params)}`,
      ),
    activity: (orgId: string, cursor?: string) =>
      request<{ events: ActivityEvent[]; next_cursor: string }>(
        `/v1/orgs/${orgId}/activity${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
      ),
    createBundleShare: (orgId: string, fileIds: string[], expiresAt?: string) =>
      request<ShareLink>(`/v1/orgs/${orgId}/shares`, {
        method: "POST",
        body: json({ file_ids: fileIds, expires_at: expiresAt }),
      }),
    usage: (orgId: string) => request<OrgUsage>(`/v1/orgs/${orgId}/usage`),
    rootFolders: (orgId: string) => request<FolderNode[]>(`/v1/orgs/${orgId}/folders`),
    trashedFolders: (orgId: string) => request<FolderNode[]>(`/v1/orgs/${orgId}/trash/folders`),
    trashedFiles: (orgId: string) => request<FileNode[]>(`/v1/orgs/${orgId}/trash/files`),
  },

  folders: {
    create: (orgId: string, name: string, parentId: string | null) =>
      request<FolderNode>(`/v1/orgs/${orgId}/folders`, { method: "POST", body: json({ name, parent_id: parentId }) }),
    children: (folderId: string) =>
      request<{ folders: FolderNode[]; files: FileSummary[] }>(`/v1/folders/${folderId}/children`),
    path: (folderId: string) => request<{ id: string; name: string }[]>(`/v1/folders/${folderId}/path`),
    rename: (folderId: string, name: string) =>
      request<FolderNode>(`/v1/folders/${folderId}`, { method: "PATCH", body: json({ name }) }),
    move: (folderId: string, parentId: string | null) =>
      request<FolderNode>(`/v1/folders/${folderId}`, { method: "PATCH", body: json({ parent_id: parentId }) }),
    trash: (folderId: string) => request(`/v1/folders/${folderId}`, { method: "DELETE" }),
    restore: (folderId: string) => request<FolderNode>(`/v1/folders/${folderId}/restore`, { method: "POST" }),
    share: (folderId: string, expiresAt?: string) =>
      request<ShareLink>(`/v1/folders/${folderId}/share`, { method: "POST", body: json({ expires_at: expiresAt }) }),
  },

  files: {
    rename: (fileId: string, name: string) =>
      request<FileNode>(`/v1/files/${fileId}`, { method: "PATCH", body: json({ name }) }),
    move: (fileId: string, folderId: string) =>
      request<FileNode>(`/v1/files/${fileId}`, { method: "PATCH", body: json({ folder_id: folderId }) }),
    trash: (fileId: string) => request(`/v1/files/${fileId}`, { method: "DELETE" }),
    restore: (fileId: string) => request<FileNode>(`/v1/files/${fileId}/restore`, { method: "POST" }),
    purge: (fileId: string) => request(`/v1/files/${fileId}/purge`, { method: "DELETE" }),
    versions: (fileId: string) => request<FileVersion[]>(`/v1/files/${fileId}/versions`),
    thumbnail: (fileId: string) => request<{ targets: string[] }>(`/v1/files/${fileId}/thumbnail`),
    downloadPlan: (fileId: string, versionId: string) =>
      request<DownloadPlan>(`/v1/files/${fileId}/versions/${versionId}/download-plan`),
    restoreVersion: (fileId: string, versionId: string) =>
      request<FileNode>(`/v1/files/${fileId}/versions/${versionId}/restore`, { method: "POST" }),
    share: (fileId: string, expiresAt?: string) =>
      request<ShareLink>(`/v1/files/${fileId}/share`, { method: "POST", body: json({ expires_at: expiresAt }) }),
  },

  shares: {
    resolve: (token: string) => request<ResolvedShare>(`/v1/shares/${token}`),
    children: (token: string, folderId: string) =>
      request<ShareChildren>(`/v1/shares/${token}/folders/${folderId}`),
    downloadPlan: (token: string, fileId: string) =>
      request<{ file: ShareFileInfo; download_plan: DownloadPlan }>(
        `/v1/shares/${token}/files/${fileId}/download-plan`,
      ),
    revoke: (token: string) => request(`/v1/shares/${token}`, { method: "DELETE" }),
  },

  uploads: {
    // Upload-scoped (audit §05: proof-of-possession) — call after init, not
    // before; the server needs the upload's org to answer "what does *this
    // org* still need to upload".
    checkChunks: (uploadId: string, hashes: string[]) =>
      request<{ missing: string[] }>(`/v1/uploads/${uploadId}/chunks/check`, {
        method: "POST",
        body: json({ hashes }),
      }),
    init: (params: { folderId?: string; fileId?: string; name?: string; sizeBytes: number; mimeType: string }) =>
      request<{ upload_id: string }>("/v1/uploads", {
        method: "POST",
        body: json({
          folder_id: params.folderId,
          file_id: params.fileId,
          name: params.name,
          size_bytes: params.sizeBytes,
          mime_type: params.mimeType,
        }),
      }),
    initChunk: (uploadId: string, hash: string) =>
      request<{ targets: ChunkTarget[]; expires_at: string }>(`/v1/uploads/${uploadId}/chunks/${hash}/init`, {
        method: "POST",
      }),
    commitChunk: (uploadId: string, hash: string, sizeBytes: number, etags: Record<string, string>) =>
      request(`/v1/uploads/${uploadId}/chunks/${hash}/commit`, {
        method: "POST",
        body: json({ size_bytes: sizeBytes, etags }),
      }),
    complete: (uploadId: string, chunkOrder: string[], sizeBytes: number, checksumSha256: string) =>
      request<{ file_id: string; version_id: string }>(`/v1/uploads/${uploadId}/complete`, {
        method: "POST",
        headers: { "Idempotency-Key": uploadId },
        body: json({ chunk_order: chunkOrder, size_bytes: sizeBytes, checksum_sha256: checksumSha256 }),
      }),
  },

  admin: {
    nodes: () => request<StorageNode[]>("/v1/admin/nodes"),
    dlq: () => request<{ events: DeadEvent[] }>("/v1/admin/dlq"),
    retryDeadEvent: (id: string) => request<{ status: string }>(`/v1/admin/dlq/${id}/retry`, { method: "POST" }),
    ring: (fileId?: string) =>
      request<RingInfo>(`/v1/admin/ring${fileId ? `?file_id=${encodeURIComponent(fileId)}` : ""}`),
  },
};
