# API Design — Nimbus Storage Platform

Status: DRAFT
Version: 0.1
Depends on: [03-hld.md](03-hld.md) §2 (error model, middleware), [05-database-design.md](05-database-design.md)

REST/JSON, versioned under `/v1`. Auth via `Authorization: Bearer <access_token>` except where marked public. All list endpoints are cursor-paginated (`?cursor=&limit=`, response includes `next_cursor`).

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
| 429 | `rate_limited` |
| 500 | `internal` |

Mutating endpoints that must be safe to retry accept an `Idempotency-Key` header (see LLD §2/§3).

## 2. Auth

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/auth/register` | `{email, password}` | 201 `{user_id, email}` | public |
| POST | `/v1/auth/login` | `{email, password}` | 200 `{access_token, refresh_token, expires_in}` | public |
| POST | `/v1/auth/refresh` | `{refresh_token}` | 200 `{access_token, refresh_token, expires_in}` | public; rotates token (LLD §3) |
| POST | `/v1/auth/logout` | `{refresh_token}` | 204 | blacklists access token jti + revokes refresh family |

## 3. Orgs & membership

| Method | Path | Body | 2xx |
|---|---|---|---|
| POST | `/v1/orgs` | `{name}` | 201 `{id, name, owner_user_id}` |
| GET | `/v1/orgs/{orgId}/members` | — | 200 `[{user_id, email, role}]` |
| POST | `/v1/orgs/{orgId}/members` | `{email, role}` | 201 `{org_id, user_id, role}` — owner only |
| DELETE | `/v1/orgs/{orgId}/members/{userId}` | — | 204 — owner only |

## 4. Folders & files (metadata)

| Method | Path | Body | 2xx |
|---|---|---|---|
| POST | `/v1/orgs/{orgId}/folders` | `{parent_id, name}` | 201 `{id, name, parent_id}` |
| GET | `/v1/folders/{folderId}/children` | — | 200 `{folders:[...], files:[...]}` |
| PATCH | `/v1/folders/{folderId}` | `{name?, parent_id?}` | 200 — rename/move, one transaction |
| DELETE | `/v1/folders/{folderId}` | — | 204 — soft delete (trash), cascades to children |
| PATCH | `/v1/files/{fileId}` | `{name?, folder_id?}` | 200 |
| DELETE | `/v1/files/{fileId}` | — | 204 — soft delete (trash) |
| POST | `/v1/files/{fileId}/restore` | — | 200 — undo trash |
| DELETE | `/v1/files/{fileId}/purge` | — | 204 — permanent, irreversible |
| GET | `/v1/files/{fileId}/versions` | — | 200 `[{id, size_bytes, checksum, created_at}]` |
| POST | `/v1/files/{fileId}/versions/{versionId}/restore` | — | 200 — makes this version latest (creates no new bytes, just repoints) |

## 5. Upload (chunked/resumable/dedup — the FR-6..FR-11 flow)

| Method | Path | Body | 2xx |
|---|---|---|---|
| POST | `/v1/chunks/check` | `{hashes: [sha256...]}` | 200 `{missing: [sha256...]}` |
| POST | `/v1/uploads` | `{folder_id, name, size_bytes, mime_type}` | 201 `{upload_id}` |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/init` | — | 200 `{targets: [{node_id, put_url}], expires_at}` — always 2 targets (N=2) |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/commit` | `{etags: {node_id: etag}}` | 200 `{status: "committed"}` — 409 `conflict` code `checksum_mismatch` on integrity failure |
| POST | `/v1/uploads/{uploadId}/complete` | `{chunk_order: [hash, ...]}` + `Idempotency-Key` header | 201 `{file_id, version_id}` |

Resumability: a client that drops mid-upload just re-calls `/chunks/check` on reconnect — already-committed chunks come back as not-missing, so only the remainder gets re-sent. No separate "resume" endpoint needed; this is a deliberate simplification.

## 6. Download

| Method | Path | 2xx |
|---|---|---|
| GET | `/v1/files/{fileId}/versions/{versionId}/download-plan` | 200 `{chunks: [{sequence, hash, targets: [get_url, get_url_fallback]}]}` |

Client fetches the plan once, then issues direct GETs to storage nodes per chunk (falling back to the second URL on failure), reassembling client-side. Keeping this a "plan" endpoint rather than a redirect/proxy is what lets bytes flow directly client↔storage without the API in the hot path (matches System Design §2.4).

## 7. Sharing

| Method | Path | Body | 2xx |
|---|---|---|---|
| POST | `/v1/files/{fileId}/share` | `{expires_at?}` | 201 `{token, url}` |
| GET | `/v1/shares/{token}` | — | 200 `{file: {...}, download_plan: {...}}` — **public**, no auth header |
| DELETE | `/v1/shares/{token}` | — | 204 |

## 8. Search & activity

| Method | Path | 2xx |
|---|---|---|
| GET | `/v1/orgs/{orgId}/search?q=&type=&owner=&date_from=&date_to=&size_min=&size_max=` | 200 `{results: [...], next_cursor}` |
| GET | `/v1/orgs/{orgId}/activity?cursor=&limit=` | 200 `{events: [{verb, target_type, target_id, actor, created_at}], next_cursor}` |

## 9. Admin

| Method | Path | 2xx |
|---|---|---|
| GET | `/v1/admin/nodes` | 200 `[{id, endpoint, status, last_heartbeat_at}]` — this is what's on screen during the chaos demo |
| GET | `/v1/admin/orgs/{orgId}/usage` | 200 `{total_bytes, file_count}` |

## 10. Explicitly not versioned as gRPC

Per SRS non-goals — REST only in v1. Module boundaries (HLD §1) are clean enough that a gRPC facade could be added later without touching business logic, but it's not built now.

## 11. Open question for sign-off

`/v1/files/{fileId}/versions/{versionId}/restore` — restoring an old version as "latest" repoints `latest_version_id` rather than duplicating chunks/bytes (cheap, and versions form a simple list, not a branching tree). Confirming that's the semantics you want (vs., say, creating a brand-new version that happens to have the old content) before it's load-bearing in the schema.
