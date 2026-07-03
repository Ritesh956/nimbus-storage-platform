# Distributed Architecture Detail — Nimbus Storage Platform

Status: DRAFT
Version: 0.1
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
| Failure path | after 5 failed deliveries, message routed to `nimbus.uploads.completed.dlq` (a second consumer just logs + increments a Prometheus counter — no automated remediation in v1, intentionally: a human looks at the DLQ) |

Event payload: `{event_id, file_id, version_id, org_id, request_id}` — `request_id` is the correlation ID carried from the original HTTP request (HLD §2) so a DLQ'd message can be traced back to the upload that produced it.

## 4. Consistent hashing parameters

- Hash function: SHA-1 truncated to uint32 (simple, well-understood, not a performance bottleneck at this node count — no need for a faster non-cryptographic hash like murmur3 here).
- 128 virtual nodes per physical node — enough to keep chunk distribution reasonably even across 3-5 physical nodes without the vnode table becoming unwieldy to reason about in a demo/explanation.
- Ring rebuild cost: O(physical_nodes × 128 × log) — trivial at this scale; irrelevant until physical node count is in the thousands, noted here only because "how expensive is a rebuild" is a natural follow-up question.

## 5. Chaos test (SRS FR-21 — a real deliverable, not a design claim)

`scripts/chaos-node-kill.sh`, run against the Compose stack:

1. Start a background upload of a multi-chunk (~50 MB, 8 MiB chunks) test file via the CLI/`curl` test client.
2. Mid-upload (after chunk 2 of ~7 commits), `docker stop nimbus-minio-2`.
3. Assert the upload still reaches `201` on `/complete` — remaining chunks route to surviving replicas per §1.2 placement, since `Resolve` re-evaluates health on every call.
4. Immediately download the completed file; assert byte-for-byte match against the original (checksum compare) — proves reads correctly avoided the dead node.
5. Query `/v1/admin/nodes`; assert `nimbus-minio-2` shows `status: down` within the NFR-3 bound (≤10s from stop to reflected status).
6. `docker start nimbus-minio-2`; assert status flips back to `healthy` within one health-check interval after it's reachable again.
7. Print a pass/fail summary per assertion — this script **is** the demo artifact referenced in the SRS Definition of Done, run live or recorded for the README.

This script is a build deliverable (Phase 10 roadmap), not just documentation — it's what makes "handles node failure" a verified claim instead of an assertion.

## 6. Open question for sign-off

Vnode count (128) and hash truncation (SHA-1→uint32) are implementation defaults with no real tradeoff at this node count — flagging only in case you want a specific number to cite/defend in an interview (e.g. "why 128 and not 256"), otherwise proceeding as-is.
