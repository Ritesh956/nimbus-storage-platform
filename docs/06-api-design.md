# API Design — Nimbus Storage Platform

Status: **current as of the Tier 1 backlog session (post-Day-15)** — this doc is kept in sync with `backend/cmd/api/main.go`'s actual route table; every endpoint below exists and works. Re-verified route-by-route against `main.go` on Day 15's SRS DoD pass — no drift found. The Tier 1 session added `GET /v1/folders/{folderId}/path`, `GET /v1/files/{fileId}/thumbnail`, and version metadata on the children listing (§4).
Version: 0.3
Depends on: [03-hld.md](03-hld.md) §2 (error model, middleware), [05-database-design.md](05-database-design.md)

REST/JSON, versioned under `/v1`. Auth via `Authorization: Bearer <access_token>` except where marked public. CORS is enabled (`httpserver.CORS`, added Day 10 for the browser frontend) — permissive by default (`NIMBUS_CORS_ORIGIN=*`), safe here because auth is a Bearer token, not cookies.

## 1. Conventions

**Error envelope** (every non-2xx):
```json
{ "error": { "code": "not_found", "message": "file not found", "request_id": "a1b2c3" } }
```
| HTTP | code |
|---|---|
| 400 | `invalid` |
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 409 | `conflict` |
| 413 | `too_large` (upload exceeds `NIMBUS_MAX_UPLOAD_BYTES`, default 100 MiB — Tier 2 session) |
| 429 | `rate_limited` (per-caller Redis token bucket, default 25 rps / burst 50 — built in the Tier 2 session; keyed by user ID for authenticated callers, client IP otherwise; `/healthz`, `/readyz`, `/metrics` exempt; fails open if Redis is down) |
| 500 | `internal` |
| 507 | `quota_exceeded` (upload would push the org past `NIMBUS_ORG_QUOTA_BYTES`, default 10 GiB — Tier 2 session) |

Mutating endpoints that must be safe to retry accept an `Idempotency-Key` header (currently only `/v1/uploads/{uploadId}/complete`).

Cursor pagination (`?cursor=&limit=`, response includes `next_cursor`) is implemented for `/v1/orgs/{orgId}/search` and `/v1/orgs/{orgId}/activity`.

## 2. Auth — `internal/auth`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/auth/register` | `{email, password}` | 201 `{user_id, email}` | public |
| POST | `/v1/auth/login` | `{email, password}` | 200 `{access_token, refresh_token, expires_in}` | public |
| POST | `/v1/auth/refresh` | `{refresh_token}` | 200 `{access_token, refresh_token, expires_in}` | public; rotates token, reuse-of-old-token revokes the whole family (LLD §3) |
| POST | `/v1/auth/logout` | `{refresh_token}` | 204 | blacklists access token jti in Redis + revokes refresh family |

Login, refresh, and logout above each also write a row to `auth_audit_log` (FR-4, Day 15) — best-effort, not part of any response body, no API surface of its own (queried directly in Postgres, not exposed via a route). `register` does not.

## 3. Orgs & membership — `internal/org`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/orgs` | `{name}` | 201 `{id, name, owner_user_id}` | also auto-creates a "Home" root folder (best-effort, non-fatal — see §4) |
| GET | `/v1/orgs` | — | 200 `[{id, name, owner_user_id}]` | orgs the caller belongs to; added Day 10 (no prior way to discover orgs after login) |
| GET | `/v1/orgs/{orgId}/members` | — | 200 `[{user_id, email, role}]` | any member |
| POST | `/v1/orgs/{orgId}/members` | `{email, role}` | 201 `{org_id, user_id, role}` | owner only; 404 if email isn't a registered user |
| DELETE | `/v1/orgs/{orgId}/members/{userId}` | — | 204 | owner only; 409 if target is the org owner |

## 4. Folders & files (metadata) — `internal/folder`, `internal/file`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/orgs/{orgId}/folders` | `{parent_id, name}` | 201 `{id, org_id, parent_id, name, created_at, updated_at}` | |
| GET | `/v1/orgs/{orgId}/folders` | — | 200 `[{id, org_id, parent_id, name, created_at, updated_at}]` | root-level (`parent_id IS NULL`) folders; added Day 10 — every other listing endpoint needs an already-known folder ID |
| GET | `/v1/folders/{folderId}/children` | — | 200 `{folders: [...], files: [{id, name, size_bytes, mime_type, has_thumbnail}]}` | file entries carry latest-version display metadata (Tier 1 session — was name-only) so the browser renders rows, and knows to fetch thumbnails, without per-file round-trips |
| GET | `/v1/folders/{folderId}/path` | — | 200 `[{id, name}]` | ancestor chain root→self inclusive, for breadcrumbs (Tier 1 session) |
| PATCH | `/v1/folders/{folderId}` | `{name?, parent_id?}` | 200 folder | rename/move, one transaction; rejects cycles |
| DELETE | `/v1/folders/{folderId}` | — | 204 | soft delete (trash), cascades to descendant folders + files |
| POST | `/v1/folders/{folderId}/restore` | — | 200 folder | undoes trash, cascades to what was trashed with it |
| GET | `/v1/orgs/{orgId}/trash/folders` | — | 200 `[{id, org_id, parent_id, name, ...}]` | added Day 10 |
| PATCH | `/v1/files/{fileId}` | `{name?, folder_id?}` | 200 `{id, org_id, folder_id, name, latest_version_id, created_at, updated_at}` | |
| DELETE | `/v1/files/{fileId}` | — | 204 | soft delete (trash) |
| POST | `/v1/files/{fileId}/restore` | — | 200 file | undo trash |
| DELETE | `/v1/files/{fileId}/purge` | — | 204 | permanent; 400 if not already trashed |
| GET | `/v1/orgs/{orgId}/trash/files` | — | 200 `[{id, org_id, folder_id, name, ...}]` | added Day 10 |
| GET | `/v1/files/{fileId}/versions` | — | 200 `[{id, size_bytes, checksum_sha256, mime_type, created_at}]` | newest first |
| GET | `/v1/files/{fileId}/thumbnail` | — | 200 `{targets: [url, ...]}` | presigned GETs for the latest version's worker-generated thumbnail, ring-preference order healthy-first; 404 until the worker has produced one. Unlike chunks, a thumbnail's node isn't recorded in Postgres — the client walks the targets like a download plan's (Tier 1 session) |
| GET | `/v1/files/{fileId}/versions/{versionId}/download-plan` | — | 200 see §6 | |
| POST | `/v1/files/{fileId}/versions/{versionId}/restore` | — | 200 file | repoints `latest_version_id`, no bytes duplicated |

**Note:** there is no bare `POST /v1/files` — a file only ever comes into existence via a completed upload (§5).

## 5. Upload (chunked/resumable/dedup) — `internal/upload`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/chunks/check` | `{hashes: [sha256hex...]}` | 200 `{missing: [sha256hex...]}` | global dedup check, no org scoping |
| POST | `/v1/uploads` | `{folder_id, name, size_bytes, mime_type}` **or** `{file_id, size_bytes, mime_type}` | 201 `{upload_id}` | `file_id` form uploads a new version of an existing file instead of creating one (added Day 6); `folder_id`/`name` are derived from the target file in that case |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/init` | — | 200 `{targets: [{node_id, put_url}], expires_at}` | N targets (replication factor, default 2) |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/commit` | `{size_bytes, etags: {node_id: etag}}` | 200 `{status: "committed"}` | 409 if replica ETags disagree (corruption signal) |
| POST | `/v1/uploads/{uploadId}/complete` | `{chunk_order: [hash...], size_bytes, checksum_sha256}` + `Idempotency-Key` header | 201 `{file_id, version_id}` | also (best-effort) records a `uploaded` activity event and publishes `nimbus.uploads.completed` to NATS |

Resumability: a client that drops mid-upload re-calls `/chunks/check` on reconnect — already-committed chunks come back as not-missing. No separate "resume" endpoint.

**Limits (Tier 2 session):** `POST /v1/uploads` rejects `size_bytes` over `NIMBUS_MAX_UPLOAD_BYTES` (413 `too_large`) or past the org's `NIMBUS_ORG_QUOTA_BYTES` (507 `quota_exceeded`; usage = sum of every stored version's bytes org-wide, trashed included — purge is what frees quota). `/complete` re-checks both against the completed size, since nothing forces it to match what init declared. 0 disables either limit.

## 6. Download

| Method | Path | 2xx |
|---|---|---|
| GET | `/v1/files/{fileId}/versions/{versionId}/download-plan` | 200 `{chunks: [{sequence, hash, targets: [url, ...]}]}` |

Targets are presigned GET URLs to the chunk's *actually recorded* replica locations (not a fresh ring lookup), healthy-first, so the client's primary attempt is likeliest to succeed with a fallback still available.

## 7. Sharing — `internal/sharing`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/files/{fileId}/share` | `{expires_at?}` (RFC3339) | 201 `{token, url}` | `url` is the *backend's own* `/v1/shares/{token}` — the frontend deliberately ignores it and builds `{origin}/shares/{token}` itself (see docs/00-project-state.md known issues) |
| GET | `/v1/shares/{token}` | — | 200 `{file: {id, name, size_bytes, mime_type, checksum_sha256}, download_plan: {...}}` | **public**, no auth header; 403 if expired, 404 if revoked/unknown |
| DELETE | `/v1/shares/{token}` | — | 204 | requires org membership on the underlying file (resolved internally, since the route has no `{fileId}`) |

## 8. Search & activity

| Method | Path | 2xx | Notes |
|---|---|---|---|
| GET | `/v1/orgs/{orgId}/search?q=&type=&owner=&date_from=&date_to=&size_min=&size_max=&cursor=&limit=` | 200 `{results: [{file_id, name, folder_id, owner_id, created_at, size_bytes, mime_type}], next_cursor}` | `type` is a prefix match on mime_type (e.g. `image` matches `image/png`) |
| GET | `/v1/orgs/{orgId}/activity?cursor=&limit=` | 200 `{events: [{verb, target_type, target_id, actor, created_at}], next_cursor}` | `verb` is `"uploaded"` (written synchronously by nimbus-api) or `"thumbnail_generated"` (written by nimbus-worker, `actor: null`) |

## 9. Admin — `internal/storage`, `internal/events`

| Method | Path | 2xx | Notes |
|---|---|---|---|
| GET | `/v1/admin/nodes` | 200 `[{id, endpoint, status, last_heartbeat_at}]` | this is what's on screen during the chaos demo; requires auth only, no platform-admin role exists |
| GET | `/v1/admin/dlq` | 200 `{events: [{id, subject, payload, error, deliveries, status, created_at, retried_at?}]}` | dead-lettered NATS events (newest first, cap 100) — Tier 2 session; same auth-only posture as `/admin/nodes` |
| POST | `/v1/admin/dlq/{id}/retry` | 200 `{status: "retried"}` | republishes the stored payload to its original subject; 409 if already retried |

**Not implemented**: per-org usage (`GET /v1/admin/orgs/{orgId}/usage`, total bytes/file count) — was sketched in an earlier draft of this doc but never built; no roadmap day currently calls for it. `internal/admin` is a reserved, empty package (see docs/08-folder-structure.md).

## 10. Explicitly not versioned as gRPC

Per SRS non-goals — REST only. Module boundaries (HLD §1) are clean enough that a gRPC facade could be added later without touching business logic.

## 11. Resolved design decisions (formerly "open questions")

- **Version restore semantics**: repoints `latest_version_id` rather than duplicating chunks/bytes (versions form a simple list, not a branching tree) — confirmed, implemented, tested (docs/09-roadmap.md Day 6).
- **HS256 JWT** (not RS256/JWKS) — confirmed; revisit only if auth is ever split into its own service (docs/04-lld.md §3/§6).
- **Storage node vnode count (128) / SHA-1 hash** — confirmed as implementation defaults, no real tradeoff at current node count (docs/07-distributed-architecture.md §4).
