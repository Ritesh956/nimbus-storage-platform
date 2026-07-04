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

export interface ResolvedShare {
  file: {
    id: string;
    name: string;
    size_bytes: number;
    mime_type: string;
    checksum_sha256: string;
  };
  download_plan: DownloadPlan;
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

export interface StorageNode {
  id: string;
  endpoint: string;
  status: "healthy" | "down";
  last_heartbeat_at: string | null;
}
