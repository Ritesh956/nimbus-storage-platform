export interface Organization {
  id: string;
  name: string;
  owner_user_id: string;
}

export interface Member {
  user_id: string;
  email: string;
  role: "owner" | "member";
}

export interface FolderNode {
  id: string;
  org_id: string;
  parent_id: string | null;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface FileSummary {
  id: string;
  name: string;
  size_bytes: number | null;
  mime_type: string | null;
  has_thumbnail: boolean;
}

export interface FileNode {
  id: string;
  org_id: string;
  folder_id: string;
  name: string;
  latest_version_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface FileVersion {
  id: string;
  size_bytes: number;
  checksum_sha256: string;
  mime_type: string;
  created_at: string;
}

export interface DownloadPlanChunk {
  sequence: number;
  hash: string;
  targets: string[];
}

export interface DownloadPlan {
  chunks: DownloadPlanChunk[];
}

export interface ChunkTarget {
  node_id: string;
  put_url: string;
}

export interface ShareLink {
  token: string;
  url: string;
}

export interface ShareFileInfo {
  id: string;
  name: string;
  size_bytes: number;
  mime_type: string;
  checksum_sha256: string;
}

export interface ShareFolderInfo {
  id: string;
  name: string;
}

// Discriminated by kind (share scopes, post-Tier-3 session): a single file
// embeds its download plan; folder and bundle shares list entries, and each
// file's plan is fetched on demand via shares.downloadPlan.
export type ResolvedShare =
  | { kind: "file"; file: ShareFileInfo; download_plan: DownloadPlan }
  | { kind: "folder"; folder: ShareFolderInfo; folders: ShareFolderInfo[]; files: ShareFileInfo[] }
  | { kind: "bundle"; files: ShareFileInfo[] };

export interface ShareChildren {
  folder: ShareFolderInfo;
  folders: ShareFolderInfo[];
  files: ShareFileInfo[];
}

export interface SearchResult {
  file_id: string;
  name: string;
  folder_id: string;
  owner_id: string;
  created_at: string;
  size_bytes: number | null;
  mime_type: string | null;
}

export interface ActivityEvent {
  verb: string;
  target_type: string;
  target_id: string;
  actor: string | null;
  created_at: string;
}

export interface DeadEvent {
  id: string;
  subject: string;
  payload: Record<string, unknown>;
  error: string;
  deliveries: number;
  status: "dead" | "retried";
  created_at: string;
  retried_at?: string;
}

export interface StorageNode {
  id: string;
  endpoint: string;
  status: "healthy" | "down";
  last_heartbeat_at: string | null;
}

export interface RingVNode {
  position: number; // point on the uint32 hash ring
  node: string;
}

export interface RingChunk {
  sequence: number;
  hash: string;
  position: number;
  preference: string[]; // ring walk order, health-ignoring
  locations: string[]; // where the chunk was actually committed
}

export interface RingInfo {
  vnodes: RingVNode[];
  replication_factor: number;
  chunks?: RingChunk[]; // present when ?file_id= was given
}
