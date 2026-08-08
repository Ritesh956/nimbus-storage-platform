// Re-exports the shapes nimbus-api's handlers actually produce, generated
// (roadmap #11) from backend/internal/apidoc's DTOs via
// backend/cmd/openapi-gen -> docs/openapi.json -> openapi-typescript ->
// api-schema.generated.ts. Regenerate both with:
//   (cd backend && go run ./cmd/openapi-gen -out ../docs/openapi.json)
//   (cd frontend && npx openapi-typescript ../docs/openapi.json -o lib/api-schema.generated.ts)
// This file exists so every other frontend file keeps importing familiar
// names (FolderNode, FileSummary, ...) from "@/lib/types" rather than
// reaching into api-schema.generated.ts's components["schemas"] directly —
// and to restore the handful of TypeScript string-literal unions
// (OrgRole, share "kind" discriminants, DeadEvent/StorageNode status) that
// don't survive the round trip through Go (which has no enum type) and
// OpenAPI's plain `type: string` as a result. Everything else below is a
// direct type alias, not hand-maintained.
import type { components } from "./api-schema.generated";

export type Organization = components["schemas"]["Organization"];

// Org RBAC ladder (governance session): owner > admin > member. Admin is
// delegated governance — usage view + bounded member management. Not
// derived — see this file's header comment.
export type OrgRole = "owner" | "admin" | "member";

export type Member = Omit<components["schemas"]["Member"], "role"> & { role: OrgRole };

// Owner-gated org oversight (GET /v1/orgs/{orgId}/usage) — aggregate
// action metadata only; see the backend's org/usage.go for the privacy line.
export type OrgUsage = Omit<components["schemas"]["OrgUsage"], "members"> & {
  members: (Omit<components["schemas"]["OrgUsage"]["members"][number], "role"> & { role: OrgRole })[];
};

export type FolderNode = components["schemas"]["FolderNode"];
export type FileSummary = components["schemas"]["FileSummary"];
export type FileNode = components["schemas"]["FileNode"];
export type FileVersion = components["schemas"]["FileVersion"];
export type DownloadPlanChunk = components["schemas"]["DownloadPlanChunk"];
export type DownloadPlan = components["schemas"]["DownloadPlan"];
export type ChunkTarget = components["schemas"]["ChunkTarget"];
export type ShareLink = components["schemas"]["ShareLink"];
export type ShareFileInfo = components["schemas"]["ShareFileInfo"];
export type ShareFolderInfo = components["schemas"]["ShareFolderInfo"];

// Discriminated by kind (share scopes, post-Tier-3 session): a single file
// embeds its download plan; folder and bundle shares list entries, and each
// file's plan is fetched on demand via shares.downloadPlan. Built from the
// three generated ResolvedShare* schemas with the literal "kind" restored
// (OpenAPI's oneOf doesn't carry a discriminant back as a TS literal type).
export type ResolvedShare =
  | ({ kind: "file" } & Omit<components["schemas"]["ResolvedShareFile"], "kind">)
  | ({ kind: "folder" } & Omit<components["schemas"]["ResolvedShareFolder"], "kind">)
  | ({ kind: "bundle" } & Omit<components["schemas"]["ResolvedShareBundle"], "kind">);

export type ShareChildren = components["schemas"]["ShareChildrenResponse"];
export type SearchResult = components["schemas"]["SearchResult"];
export type ActivityEvent = components["schemas"]["ActivityEvent"];

export type DeadEvent = Omit<components["schemas"]["DeadEvent"], "status"> & { status: "dead" | "retried" };
export type StorageNode = Omit<components["schemas"]["StorageNode"], "status"> & { status: "healthy" | "down" };

export type RingVNode = components["schemas"]["RingVNode"];
export type RingChunk = components["schemas"]["RingChunk"];
export type RingInfo = components["schemas"]["RingInfo"];
