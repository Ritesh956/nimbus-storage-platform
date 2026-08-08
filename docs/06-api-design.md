# API Design — Nimbus Storage Platform

Status: **current as of the §03/§04/§05 audit-fix session (2026-08-08)** for prose/rationale, but as of the roadmap #11 session (2026-08-08, same day) **the route-by-route reference below is no longer the source of truth** — [`docs/openapi.json`](openapi.json) is, generated from `cmd/api/*.go`'s real route registrations by `backend/cmd/openapi-gen` (CI fails if it's stale). This doc previously required a manual route-by-route re-check against `main.go` each session (last done on Day 15's SRS DoD pass, and again for the audit's own §07 finding) — that discipline is now enforced by the generator instead of relied on by habit. If a route below and `openapi.json` ever disagree, `openapi.json` wins; the tables below are kept for the prose (auth flow order, error-code rationale, idempotency/pagination notes) that a schema alone doesn't carry. Frontend types are derived the same way: `frontend/lib/types.ts` re-exports from `frontend/lib/api-schema.generated.ts`, itself generated from `openapi.json` via `openapi-typescript` — see that file's header for the regenerate commands.
Version: 0.8
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
| POST | `/v1/auth/login` | `{email, password}` | 200 `{access_token, refresh_token, expires_in}` **or** 200 `{totp_required: true, challenge_token}` | public; the second shape is returned when the account has confirmed TOTP (Tier 4) — finish via `/v1/auth/login/totp` within 5 minutes |
| POST | `/v1/auth/login/totp` | `{challenge_token, code}` | 200 `{access_token, refresh_token, expires_in}` | public; step two of a TOTP-gated login. A wrong code leaves the challenge alive (retype within the TTL); success consumes it. 401 on bad/expired challenge or code |
| POST | `/v1/auth/refresh` | `{refresh_token}` | 200 `{access_token, refresh_token, expires_in}` | public; rotates token, reuse-of-old-token revokes the whole family (LLD §3) |
| POST | `/v1/auth/logout` | `{refresh_token}` | 204 | blacklists access token jti in Redis + revokes refresh family |
| POST | `/v1/auth/password/forgot` | `{email}` | 202 | public; always 202 whether or not the email is registered (no enumeration oracle). Emails a single-use reset link (1h TTL, SHA-256 of the token stored) via SMTP (`NIMBUS_SMTP_ADDR` — Mailpit in compose); with no SMTP configured the link is logged instead of sent |
| POST | `/v1/auth/password/reset` | `{token, password}` | 204 | public; one transaction: consumes the token, rewrites the password hash, revokes **all** the user's refresh-token families. 400 on used/expired/unknown token |
| GET | `/v1/auth/me` | — | 200 `{user_id, email, is_platform_admin}` | the caller's own identity — lets the frontend decide what to render (e.g. the Admin nav) without probing gated routes (governance session) |
| GET | `/v1/auth/totp` | — | 200 `{enabled}` | whether the caller has confirmed TOTP |
| POST | `/v1/auth/totp/setup` | — | 200 `{secret, otpauth_uri}` | starts (or restarts) a pending enrollment — login is not gated until confirmed; a confirmed enrollment is never overwritten. 409 if already enabled |
| POST | `/v1/auth/totp/confirm` | `{code}` | 204 | verifies a current code and flips 2FA on. 400 bad code, 409 no pending enrollment |
| DELETE | `/v1/auth/totp` | `{code}` | 204 | requires a current code so a hijacked session alone can't switch 2FA off. 400 bad code, 404 not enabled |

TOTP (Tier 4, backlog #15) is RFC 6238 — HMAC-SHA1, 6 digits, 30s steps, ±1 step of clock skew — implemented on the Go stdlib (`internal/auth/totp.go`, locked to the RFC's Appendix B vectors by unit tests), with a Redis-backed one-use-per-time-step replay guard shared by login/confirm/disable.

Login, refresh, and logout above each also write a row to `auth_audit_log` (FR-4, Day 15) — best-effort, not part of any response body, no API surface of its own (queried directly in Postgres, not exposed via a route). Tier 4 added `password_reset`, `totp_enabled`, and `totp_disabled` verbs; a TOTP-gated login records its `login` row when the second step succeeds. `register` does not write one.

## 3. Orgs & membership — `internal/org`

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/orgs` | `{name}` | 201 `{id, name, owner_user_id}` | also auto-creates a "Home" root folder (best-effort, non-fatal — see §4) |
| GET | `/v1/orgs` | — | 200 `[{id, name, owner_user_id}]` | orgs the caller belongs to; added Day 10 (no prior way to discover orgs after login) |
| GET | `/v1/orgs/{orgId}/members` | — | 200 `[{user_id, email, role}]` | any member |
| POST | `/v1/orgs/{orgId}/members` | `{email, role}` | 201 `{org_id, user_id, role}` | admin tier and up; **admins can only grant `member`** — elevated roles (admin/owner) are owner-grantable only (403 otherwise). 404 if email isn't a registered user |
| DELETE | `/v1/orgs/{orgId}/members/{userId}` | — | 204 | admin tier and up; **admins can only remove plain members** (403 on admins/owners), and nobody removes the org creator (409) |
| GET | `/v1/orgs/{orgId}/usage` | — | 200 `{storage: {used_bytes, quota_bytes, live_files, trashed_files}, active_share_links, members: [{user_id, email, role, joined_at, last_active_at, events_30d}], activity_30d: {verb: n}}` | admin tier and up (governance session; opened to admins in part 2) — org oversight assembled via ports over file/sharing/activity. Aggregate action metadata already member-visible via the activity feed; no file names/content, and deliberately not `auth_audit_log` (spans orgs, user-private). Deliberately under `/v1/orgs/`, not `/v1/admin/` — that prefix means platform-admin cluster ops (§9) |

**Roles** (governance session part 2, migration 000013): `owner > admin > member`, compared by rank in `org.RequireRole`. `admin` is delegated governance — the usage view plus bounded member management (rules above) — while file/folder/share access is identical for every role. There is no change-role endpoint; re-granting is remove + re-add (owner-only for elevated roles).

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
| POST | `/v1/uploads` | `{folder_id, name, size_bytes, mime_type}` **or** `{file_id, size_bytes, mime_type}` | 201 `{upload_id}` | `file_id` form uploads a new version of an existing file instead of creating one (added Day 6); `folder_id`/`name` are derived from the target file in that case |
| POST | `/v1/uploads/{uploadId}/chunks/check` | `{hashes: [sha256hex...]}` | 200 `{missing: [sha256hex...]}` | dedup check, scoped to `uploadId`'s org (audit §05 proof-of-possession fix — previously a bare global lookup at `/v1/chunks/check`, before `uploadId` existed to scope it); a hash counts as present only if this org committed it before, not merely that it exists somewhere |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/init` | — | 200 `{targets: [{node_id, put_url}], expires_at}` | N targets (replication factor, default 2) |
| POST | `/v1/uploads/{uploadId}/chunks/{hash}/commit` | `{size_bytes, etags: {node_id: etag}}` | 200 `{status: "committed"}` | 409 if replica ETags disagree (corruption signal) or if fewer than `NIMBUS_WRITE_QUORUM` etags are present (quick-wins architecture session, docs/00-project-state.md item 29); 413 if `size_bytes` exceeds `NIMBUS_CHUNK_SIZE_BYTES`; also records an `org_chunk_proofs` row so this org's future dedup checks can trust the chunk |
| POST | `/v1/uploads/{uploadId}/complete` | `{chunk_order: [hash...], size_bytes, checksum_sha256}` + `Idempotency-Key` header | 201 `{file_id, version_id}` | also (best-effort) records a `uploaded` activity event and publishes `nimbus.uploads.completed` to NATS |

Resumability: the frontend persists `{upload_id}` per file fingerprint to `localStorage` and reuses it across a page reload (`frontend/lib/upload.ts`) — a client that drops mid-upload and retries re-calls `/chunks/check` on the *same* `upload_id`, and already-committed chunks come back as not-missing. No separate server-side "resume" endpoint.

**GC interaction (Tier 3 session):** `/chunks/check` reporting a chunk present also renews its GC lease (`chunks.last_seen_at`), and chunks the collector has doomed are deliberately reported *missing* — the client re-uploads those bytes and the commit resurrects the chunk. Contract-wise nothing changes for clients (`missing` means "send it"); it just may occasionally include content the store technically still holds. See docs/07-distributed-architecture.md §6.

**Limits (Tier 2 session):** `POST /v1/uploads` rejects `size_bytes` over `NIMBUS_MAX_UPLOAD_BYTES` (413 `too_large`) or past the org's `NIMBUS_ORG_QUOTA_BYTES` (507 `quota_exceeded`; usage = sum of every stored version's bytes org-wide, trashed included — purge is what frees quota). `/complete` re-checks both against the completed size, since nothing forces it to match what init declared. 0 disables either limit.

## 6. Download

| Method | Path | 2xx |
|---|---|---|
| GET | `/v1/files/{fileId}/versions/{versionId}/download-plan` | 200 `{chunks: [{sequence, hash, targets: [url, ...]}]}` |

Targets are presigned GET URLs to the chunk's *actually recorded* replica locations (not a fresh ring lookup), healthy-first, so the client's primary attempt is likeliest to succeed with a fallback still available.

## 7. Sharing — `internal/sharing`

A share link is scoped to exactly one of: a **file**, a **folder** (its whole subtree, navigable), or a **bundle** (a hand-picked file set — one link instead of one per file). Folder/bundle scopes added post-Tier-3 (migration 000009; `share_links.org_id` was also added so revoke authorization is one membership check against the link itself).

| Method | Path | Body | 2xx | Notes |
|---|---|---|---|---|
| POST | `/v1/files/{fileId}/share` | `{expires_at}` (RFC3339, **required**, must be within 7 days) | 201 `{token, url}` | `url` is the *backend's own* `/v1/shares/{token}` — the frontend deliberately ignores it and builds `{origin}/shares/{token}` itself (see docs/00-project-state.md known issues) |
| POST | `/v1/folders/{folderId}/share` | `{expires_at}` (required, must be within 7 days) | 201 `{token, url}` | folder scope; authorized by `folder.RequireAccess` |
| POST | `/v1/orgs/{orgId}/shares` | `{file_ids: [...], expires_at}` (required, must be within 7 days) | 201 `{token, url}` | bundle scope; every file must be live and in the org (400 otherwise); a bundle of one normalizes to a plain file share |
| GET | `/v1/shares/{token}` | — | 200, discriminated by `kind` | **public**. `kind:"file"` → `{file, download_plan}` (original shape); `kind:"folder"` → `{folder: {id,name}, folders: [...], files: [...]}` (direct children); `kind:"bundle"` → `{files: [...]}`. 403 if expired, 404 if revoked/unknown |
| GET | `/v1/shares/{token}/folders/{folderId}` | — | 200 `{folder, folders, files}` | **public**; navigation inside a folder share — 404 unless `{folderId}` is the shared folder or a descendant |
| GET | `/v1/shares/{token}/files/{fileId}/download-plan` | — | 200 `{file, download_plan}` | **public**; per-file download inside a folder/bundle share — plans are presigned on demand, not upfront (15-min expiry × possibly many files). 404 if the file isn't covered by the link's scope (same 404 whether it exists elsewhere or not — no probing) |
| DELETE | `/v1/shares/{token}` | — | 204 | requires membership in the link's own org (`share_links.org_id`) |

## 8. Search & activity

| Method | Path | 2xx | Notes |
|---|---|---|---|
| GET | `/v1/orgs/{orgId}/search?q=&type=&owner=&date_from=&date_to=&size_min=&size_max=&cursor=&limit=` | 200 `{results: [{file_id, name, folder_id, owner_id, created_at, size_bytes, mime_type}], next_cursor}` | `type` is a prefix match on mime_type (e.g. `image` matches `image/png`) |
| GET | `/v1/orgs/{orgId}/activity?cursor=&limit=` | 200 `{events: [{verb, target_type, target_id, actor, created_at}], next_cursor}` | `verb` is `"uploaded"` (written synchronously by nimbus-api) or `"thumbnail_generated"` (written by nimbus-worker, `actor: null`) |
| GET | `/v1/orgs/{orgId}/events` | 200 `text/event-stream` | SSE live updates (Tier 3 session, backlog #12; `internal/live`). Frames: `activity` (`{verb, target_type, target_id}` — relayed from Redis pub/sub, so worker-written events arrive too) and `node_health` (`{node, status}` on health *transitions*). Thin revalidation signals, not full payloads — the frontend mutates its SWR caches on receipt. Consumed via `fetch` + stream reader, not EventSource (which can't send the Authorization header); keepalive comment every 25s |

## 9. Admin — `internal/storage`, `internal/events`

All five routes below are **platform-admin gated** since the governance session (`auth.RequirePlatformAdmin`, 403 otherwise — previously any valid JWT sufficed): they're deployment-wide cluster reads/actions (nodes, ring, repair, DLQ across all orgs), not org data, so the check is `users.is_platform_admin` (migration 000012, set only via the single seeded admin account — `NIMBUS_ADMIN_EMAIL`/`NIMBUS_ADMIN_PASSWORD`, `auth.Repository.EnsureSeededAdmin` — promote-only, revoke is a manual UPDATE), not an org role.

| Method | Path | 2xx | Notes |
|---|---|---|---|
| GET | `/v1/admin/nodes` | 200 `[{id, endpoint, status, last_heartbeat_at}]` | this is what's on screen during the chaos demo |
| GET | `/v1/admin/dlq` | 200 `{events: [{id, subject, payload, error, deliveries, status, created_at, retried_at?}]}` | dead-lettered NATS events (newest first, cap 100) — Tier 2 session |
| POST | `/v1/admin/dlq/{id}/retry` | 200 `{status: "retried"}` | republishes the stored payload to its original subject; 409 if already retried |
| GET | `/v1/admin/ring?file_id=` | 200 `{vnodes: [{position, node}], replication_factor, chunks?: [{sequence, hash, position, preference, locations}]}` | Tier 3 session (backlog #13): the live ring's vnode table (positions on the uint32 ring, same hashing placement uses). With `file_id`, adds the latest version's chunks — `preference` is today's health-ignoring ring walk, `locations` is where the bytes were actually committed; they diverge after a failover write. Rendered as the admin page's ring diagram |
| POST | `/v1/admin/nodes/{nodeId}/repair` | 200 `{checked, restored, unrepairable}` | Architecture-gap session (audit §02): re-verifies every chunk recorded as committed on `nodeId` against what's physically there, re-copying from a surviving replica when it isn't (e.g. after a volume loss — standalone MinIO has no storage-level durability underneath Nimbus's own replication, docs/02-system-design.md §1). Manual-trigger, synchronous, not automatic on node recovery — see `internal/storage/repair.go`'s doc comment for why. A chunk with no surviving replica anywhere is left `degraded`, not silently `committed` |

Per-org usage — sketched in an earlier draft of this doc as `/v1/admin/orgs/{orgId}/usage` — was **built in the governance session as `GET /v1/orgs/{orgId}/usage`** (§3): org governance is owner-gated and lives under `/v1/orgs/`, keeping this section purely cluster ops. `internal/admin` itself was never built — see docs/00-project-state.md "Known issues" for why that's now considered permanent rather than a gap (`internal/storage` and `internal/events`' own package doc comments cross-reference this too).

## 10. Explicitly not versioned as gRPC

Per SRS non-goals — REST only. Module boundaries (HLD §1) are clean enough that a gRPC facade could be added later without touching business logic.

## 11. Resolved design decisions (formerly "open questions")

- **Version restore semantics**: repoints `latest_version_id` rather than duplicating chunks/bytes (versions form a simple list, not a branching tree) — confirmed, implemented, tested (docs/09-roadmap.md Day 6).
- **HS256 JWT** (not RS256/JWKS) — confirmed; revisit only if auth is ever split into its own service (docs/04-lld.md §3/§6).
- **Storage node vnode count (128) / SHA-1 hash** — confirmed as implementation defaults, no real tradeoff at current node count (docs/07-distributed-architecture.md §4).
