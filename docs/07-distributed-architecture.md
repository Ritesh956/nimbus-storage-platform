# Distributed Architecture Detail — Nimbus Storage Platform

Status: current as of the Tier 3 backlog session (2026-07-05) — §6 (chunk GC + trash auto-purge) added as built
Version: 0.3
Depends on: [02-system-design.md](02-system-design.md) §2, [04-lld.md](04-lld.md) §1

Fills in the operational specifics System Design/LLD referenced but didn't pin down: Redis key schema, NATS config, node registration, and the chaos test that proves all of this actually works.

## 1. Node registration (static for v1, admin-extensible)

True dynamic node discovery (nodes self-announcing on a network) is more machinery than 3-5 fixed local containers justify. v1 registration:

- Nodes are declared in `NIMBUS_STORAGE_NODES` config (list of `{id, endpoint}`), read at `nimbus-api` startup, and upserted into `storage_nodes` (status defaults `healthy` until the first probe).
- `Router` builds its initial `Ring` from this set on boot.
- Runtime addition is still possible without a restart via `POST /v1/admin/nodes {id, endpoint}` (admin-only) — inserts the row, triggers a ring rebuild (§1.2 of LLD: rebuild-and-swap, not mutate-in-place). This is what "node registration" means in practice here: an explicit admin action, not automatic discovery — stated so it's not oversold as more than it is.

## 2. Redis key schema (ring/health shared state)

| Key | Type | TTL | Written by | Read by |
|---|---|---|---|---|
| `nimbus:nodes` | Set of `NodeID` | none | admin registration | Router on ring rebuild |
| `nimbus:node:{id}:status` | String (`healthy`\|`down`) | 6s, refreshed each successful probe | health-check loop | `Router.isHealthy` |
| `nimbus:node:{id}:last_heartbeat` | String (RFC3339 timestamp) | none | health-check loop | `/v1/admin/nodes` |
| `nimbus:health:changes` | Pub/Sub channel | n/a | health-check loop, on state transition only | all `nimbus-api` replicas (cache invalidation) |

Why TTL-expiry doubles as "down" detection: if the health-check loop itself dies or a network partition isolates a `nimbus-api` replica from a node, the key simply expires rather than requiring an explicit "mark down" write that might never happen — the *absence* of a fresh heartbeat is the down signal, not a separate flag. `isHealthy` treats a missing key as down.

Pub/Sub is an optimization, not the source of truth: a replica that misses a pub/sub message (e.g. restarted) still self-corrects within one health-check interval (2s) by reading `nimbus:node:{id}:status` directly, so there's no requirement for reliable delivery here.

## 3. NATS (JetStream) configuration

| | |
|---|---|
| Stream | `UPLOADS`, subjects `nimbus.uploads.*` |
| Publish subject | `nimbus.uploads.completed` on `CompleteUpload` |
| Consumer | `thumbnail-worker`, durable, explicit ack, `max_deliver=5` |
| Retry backoff | NATS redelivery backoff `[1s, 5s, 15s, 30s, 60s]` |
| Failure path | a message failing its 5th (final) delivery is dead-lettered into the Postgres `dead_events` table (payload + handler error + delivery count) and Term'd — built in the Tier 2 backlog session, **revised from the original sketch** of a `nimbus.uploads.completed.dlq` subject: a queryable table with a status column is what the admin DLQ endpoints (`GET /v1/admin/dlq`, `POST /v1/admin/dlq/{id}/retry` → republish to the original subject) actually need, and detecting the final delivery inline via `msg.Metadata().NumDelivered` avoids a second subscription while keeping the failure error in hand to store. Remediation is human-triggered retry from the admin UI, intentionally — no automated replay. |

Event payload: `{event_id, file_id, version_id, org_id, request_id}` — `request_id` is the correlation ID carried from the original HTTP request (HLD §2) so a DLQ'd message can be traced back to the upload that produced it.

## 4. Consistent hashing parameters

- Hash function: SHA-1 truncated to uint32 (simple, well-understood, not a performance bottleneck at this node count — no need for a faster non-cryptographic hash like murmur3 here).
- 128 virtual nodes per physical node — enough to keep chunk distribution reasonably even across 3-5 physical nodes without the vnode table becoming unwieldy to reason about in a demo/explanation.
- Ring rebuild cost: O(physical_nodes × 128 × log) — trivial at this scale; irrelevant until physical node count is in the thousands, noted here only because "how expensive is a rebuild" is a natural follow-up question.

## 5. Chaos test (SRS FR-21) — status: fully built (Day 12)

**Built and verified (Day 4)**: `scripts/smoke-storage.sh` — stops a given MinIO node, asserts `/v1/admin/nodes` reflects `down` within the NFR-3 bound, restarts it, asserts recovery. Run live: down-detection in ~5s, recovery in ~3s, well within the 10s bound.

**Built and verified (Day 12)**: `scripts/chaos-node-kill.js` — the fuller scenario originally sketched here: kill a node *mid-upload* (not just at rest) and assert the in-flight upload still completes, then verify a checksum-matched download. `scripts/smoke-storage.sh` only proves failure detection/recovery at rest; this proves an in-flight write survives a node dying under it.

Implemented as `.js`, not the `.sh` this section originally sketched — it needs real binary chunk PUTs plus precise control over exactly when `docker stop` fires relative to which chunk just committed, the same reason Days 5-9's chunked-upload/thumbnail smoke scripts are Node rather than bash (see `scripts/smoke-upload.js`'s header comment). Flagging the divergence here rather than silently renaming it back.

Steps (all ten assertions passed on a real run against the compose stack):
1. Start an upload of a ~52 MB (7-chunk) test file.
2. After chunk 2 of 7 commits, `docker compose stop` one storage node.
3. Wait for `/v1/admin/nodes` to report the node `down` (asserts the NFR-3 bound, ≤10s — observed ~5s) before continuing, so the rest of the run is deterministic rather than racing the ~6s (3 × 2s probe) breaker window.
4. Assert every subsequent chunk's placement (`InitChunk`) excludes the dead node.
5. Assert the upload still reaches `201` on `/complete`.
6. Download the completed file via the download-plan endpoint (healthy-first ordering) and assert a byte-for-byte checksum match.
7. Restart the node; assert `/v1/admin/nodes` reports `healthy` again.
8. Print a pass/fail summary per assertion (10/10 passed on the last verified run).

Run it: `node scripts/chaos-node-kill.js [node-2]` against a running `docker compose up` stack.

## 6. Chunk garbage collection (backlog #10, built in the Tier 3 session, 2026-07-05)

Content-addressed dedup (FR-7) means deleting a file never frees storage: purge removes the `file_version_chunks` references but the `chunks` row and MinIO objects survive forever. `nimbus-worker` runs a mark/sweep collector (`internal/gc`, `NIMBUS_GC_INTERVAL` default 10m):

- **Refcount is computed, not stored** — "is this chunk referenced?" is a `NOT EXISTS` probe against `file_version_chunks` (indexed on `chunk_hash`, migration 000007). Version rows die by `ON DELETE CASCADE` from purge, so a stored counter would need triggers to stay honest; the computed probe can't drift.
- **Mark**: a live chunk is doomed (`chunks.gc_state = 'doomed'`) once it is (a) referenced by no version, (b) not pinned by a recent in-progress upload session's `upload_chunks` attempts (24h cutoff, so abandoned sessions can't pin forever), and (c) unseen for `NIMBUS_GC_GRACE` (default 1h — deliberately above the 15-minute presign expiry).
- **The dedup check is a lease**: `POST /v1/uploads/{uploadId}/chunks/check` (and `/complete`'s validation, both via `upload.Repository.FindMissingChunksForOrg` — audit §05, this replaced a bare global `/v1/chunks/check`) touches `last_seen_at` on every chunk it reports present — telling a client "already stored, skip the upload" obligates us to keep the bytes for a grace window. Doomed chunks are reported *missing*, so the client re-uploads the bytes, and its commit **resurrects** the chunk (`UpsertGlobalChunk`'s conflict arm flips doomed→live). Presence also now requires an `org_chunk_proofs` row for the calling org, not just global existence — GC itself is unaffected (still keyed purely by `chunks.gc_state`/`last_seen_at`), but a chunk only counts as "already stored" for a client if *their* org has proven it, so a doomed chunk with a stale proof row still correctly re-uploads.
- **Sweep**: chunks doomed for a further `NIMBUS_GC_GRACE` are reaped one per transaction: re-verify doomed-and-unreferenced under `SELECT ... FOR UPDATE`, delete the MinIO objects from every recorded `chunk_locations` replica, then `DELETE` the row. A replica whose node is down aborts that chunk's transaction — the doomed row is the durable work record, retried next tick (S3 deletes are idempotent, so at-least-once is safe). The FK from `file_version_chunks` (`ON DELETE RESTRICT`) is the final arbiter: a reference created between re-verify and delete fails the sweep transaction loudly instead of orphaning a version's chunk.
- **Residual race, documented not hidden**: MinIO object writes aren't coordinated through Postgres, so a client PUT landing in the milliseconds between the sweep's in-transaction re-verify and its object deletion can still lose bytes — it requires a chunk unreferenced and unseen for a full grace window, doomed for another, then re-uploaded during the exact deletion transaction. The commit's row upsert serializes against the sweep's row lock, so the DB never disagrees with itself. Closing it fully needs generation-versioned object keys; explicitly out of scope for v1.
- Metrics: `nimbus_gc_chunks_doomed_total`, `nimbus_gc_chunks_reaped_total`, `nimbus_gc_bytes_reclaimed_total`, `nimbus_gc_sweep_failures_total`.

The same worker tick also runs **trash auto-purge** (FR-11, backlog #11): files/folders trashed longer than `NIMBUS_TRASH_RETENTION_DAYS` (default 30) are hard-deleted (`PurgeExpiredTrash` on the file/folder repositories via the `gc.TrashPurger` port), which is what frees their chunk references for the mark phase. Folder purge cascades over its whole subtree, so each expired folder is guarded by a subtree liveness check — a file individually restored inside a trashed folder keeps the folder alive.

Verified end-to-end by `scripts/smoke-gc.js` (Compose-only, needs short GC windows via a compose override): 13 assertions covering dedup protection, doom, dedup-invisibility, resurrection + post-resurrection download, physical sweep on every replica, trash auto-purge feeding GC, the folder liveness guard, and down-node at-least-once retry.

## 7. Resolved decisions

Vnode count (128) and hash truncation (SHA-1→uint32) — confirmed as implementation defaults, `internal/storage/ring.go`. No real tradeoff at this node count; would only matter citing/defending a specific number at internet scale (docs/02-system-design.md §8).
