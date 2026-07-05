import { clearTokens, getAccessToken, getRefreshToken, setTokens } from "./tokens";
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

// Concurrent 401s must not each trigger their own refresh call (that would
// race two rotations against the same refresh token and lose one) — every
// caller awaits the same in-flight refresh.
let refreshPromise: Promise<void> | null = null;

async function refresh(): Promise<void> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new ApiError(401, "unauthorized", "not logged in");
  const res = await fetch(`${BASE_URL}/v1/auth/refresh`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });
  if (!res.ok) {
    clearTokens();
    throw new ApiError(res.status, "unauthorized", "session expired");
  }
  const body = await res.json();
  setTokens(body.access_token, body.refresh_token);
}

async function request<T>(path: string, opts: RequestInit = {}, retry = true): Promise<T> {
  const headers = new Headers(opts.headers);
  const access = getAccessToken();
  if (access) headers.set("Authorization", `Bearer ${access}`);
  if (opts.body && !(opts.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...opts, headers });

  if (res.status === 401 && retry && getRefreshToken()) {
    refreshPromise ??= refresh().finally(() => {
      refreshPromise = null;
    });
    await refreshPromise;
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
    register: (email: string, password: string) =>
      request<{ user_id: string; email: string }>("/v1/auth/register", { method: "POST", body: json({ email, password }) }),
    login: async (email: string, password: string) => {
      const body = await request<{ access_token: string; refresh_token: string }>("/v1/auth/login", {
        method: "POST",
        body: json({ email, password }),
      });
      setTokens(body.access_token, body.refresh_token);
    },
    logout: async () => {
      const refreshToken = getRefreshToken();
      try {
        await request("/v1/auth/logout", { method: "POST", body: json({ refresh_token: refreshToken }) });
      } finally {
        clearTokens();
      }
    },
  },

  orgs: {
    listMine: () => request<Organization[]>("/v1/orgs"),
    create: (name: string) => request<Organization>("/v1/orgs", { method: "POST", body: json({ name }) }),
    listMembers: (orgId: string) => request<Member[]>(`/v1/orgs/${orgId}/members`),
    addMember: (orgId: string, email: string, role: "owner" | "member") =>
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
    checkChunks: (hashes: string[]) =>
      request<{ missing: string[] }>("/v1/chunks/check", { method: "POST", body: json({ hashes }) }),
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
