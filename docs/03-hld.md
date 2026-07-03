# High-Level Design — Nimbus Storage Platform

Status: current as of Day 11 — see docs/00-project-state.md for the authoritative "what's actually true right now" summary if this drifts again
Version: 0.3
Depends on: [02-system-design.md](02-system-design.md)

This doc covers what System Design didn't: internal module boundaries, cross-cutting concerns, and deployment topology. It doesn't re-draw the sequence diagrams already in Phase 3.

## 1. Module responsibility matrix (`nimbus-api`)

Hexagonal layout: each domain module exposes a small interface consumed by HTTP handlers; nothing outside a module touches its Postgres tables directly.

| Module | Owns | Public interface (high level) | Depends on |
|---|---|---|---|
| `auth` | users, sessions, JWT issuance/rotation | `Register`, `Login`, `Refresh`, `Middleware()` | `PG`, `Redis` (refresh-token blacklist) |
| `org` | organizations, membership | `CreateOrg`, `AddMember`, `RequireRole()` | `PG` |
| `folder` | folder tree | `CreateFolder`, `Move`, `Delete`, `ListChildren` | `PG` |
| `file` | files, versions, trash | `CreateVersion`, `Restore`, `SoftDelete`, `PurgeExpired` | `PG`, `storage` |
| `upload` | chunk lifecycle, dedup | `CheckChunks`, `InitChunk`, `CommitChunk`, `CompleteUpload` | `storage`, `PG` |
| `storage` (the distributed piece) | hash ring, node health, placement, presigned URL issuance | `Resolve(chunkHash) []Replica`, `Presign(replica, op)`, `HealthCheckLoop()` | `Redis`, MinIO SDK per node |
| `sharing` | share links, ACL checks | `CreateLink`, `ResolveLink`, `CanAccess` | `PG` |
| `search` | metadata query | `Search(query, filters)` | `PG` (Postgres full-text index) |
| `activity` | org activity feed | `List(orgID)`, `RecordUpload()`, `RecordThumbnail()` | `PG` |
| `processing` | nimbus-worker's domain logic: reassemble chunks, generate thumbnails | `Process(UploadCompleted)` | `storage`, `file`, `activity` |
| `events` | NATS JetStream publish/subscribe helpers | `EnsureStream()`, `Publisher`, `Subscribe()` | NATS |
| `admin` | reserved, currently empty | — | — |

**Revision (Day 8)**: `activity` is not purely a worker-written read model as originally sketched — most events are written *synchronously* by `nimbus-api` right where they happen (e.g. `uploaded`, written in `upload.Service.CompleteUpload`), since a cheap always-relevant fact shouldn't be at the mercy of async processing being up. `nimbus-worker` still owns genuinely async, processing-derived events (`thumbnail_generated`). See `internal/activity/types.go`'s package doc comment.

**`admin` never became a real module.** The one admin-facing read that exists (storage node health) lives directly in `storage.Handler.ListNodes` since it's a thin read over that module's own data. Per-org usage (bytes/file count) was sketched in early API design drafts but never built — no roadmap day calls for it yet.

`nimbus-worker` is a separate binary/module: subscribes to NATS, calls into shared `storage`, `file`, and `activity` packages directly (imported as a library, not called over the network, and worker builds its own `storage.Router` instance with its own health-check loop since health state is in-process) — this is what keeps the "extract a service later" story honest: the worker already runs as its own process today.

## 2. Cross-cutting concerns

- **AuthN/Z**: JWT access token (15 min TTL) + refresh token (7 day TTL, rotated on use, old one blacklisted in Redis). Middleware injects `user_id`/`org_id` into request context; every handler checks org membership via `org.RequireRole()`.
- **Error model**: single JSON error envelope `{code, message, request_id}`; internal errors are wrapped with `%w` and mapped to a small fixed set of API codes (`not_found`, `conflict`, `invalid`, `unauthorized`, `internal`) at the HTTP boundary only — domain code never returns HTTP status codes.
- **Idempotency**: `chunk-commit` is naturally idempotent (keyed by content hash — re-committing the same hash is a no-op). `upload-complete` accepts a client-generated idempotency key so retried "complete" calls don't double-create a `FileVersion`.
- **Resilience toward storage nodes**: calls from `storage` module to a MinIO node use a short timeout (500ms) + a simple circuit breaker per node — if a node's breaker is open (recently failed), it's treated as unhealthy immediately rather than waiting on a timeout, which is what keeps failover fast (SRS NFR-3, ≤10s).
- **Rate limiting**: **not implemented.** The original design called for a per-user Redis token bucket ahead of auth-heavy/upload-init endpoints; no roadmap day ever built it. Flagged here rather than left silently implied by its presence in this doc — a real gap if this project's scope ever expands past demo use.
- **CORS** (added Day 10, not in the original design): `httpserver.CORS` middleware, outermost in the chain so it short-circuits preflight `OPTIONS` before mux routing. Permissive by default (`NIMBUS_CORS_ORIGIN=*`) — safe because auth is a Bearer token, not cookies, so a wildcard origin has no ambient credential to leak.
- **Config**: 12-factor — all config via environment variables (`NIMBUS_*`), validated at startup with fail-fast; no config files baked into images.
- **Secrets**: local dev via `.env` (gitignored) + Docker Compose env; Kubernetes via `Secret` objects mounted as env vars (not committed — see Helm chart values).
- **Correlation**: every inbound request gets/propagates an `X-Request-ID`; it's attached to the logger (via context) and forwarded on the NATS event so a log line in the worker can be traced back to the originating upload request.
- **Graceful shutdown**: SIGTERM drains in-flight requests (context cancellation with a bounded grace period) before exit — required for clean rolling restarts under Kubernetes.
- **Health/readiness**: `/healthz` (process alive) and `/readyz` (DB + Redis + NATS reachable) — wired to k8s liveness/readiness probes.
- **Observability** (built Day 11): both `nimbus-api` and `nimbus-worker` expose `/metrics` (Prometheus client_golang). `nimbus_http_request_duration_seconds` is labeled by the *route pattern* the mux matched (e.g. `GET /v1/files/{fileId}`), not the literal path, via `mux.Handler(r)` — keeps cardinality bounded regardless of how many distinct IDs are ever requested. Also emitted: `nimbus_upload_chunks_committed_total`/`nimbus_upload_bytes_committed_total` (recorded once per verified `CommitChunk` — dedup hits, which skip `CommitChunk` entirely, correctly don't recount), `nimbus_storage_placement_failures_total` (incremented inside `Router.Resolve` on the shared code path both upload and thumbnail placement use), `nimbus_storage_node_healthy{node}` (each process's own in-memory health view — `nimbus-api` and `nimbus-worker` each report their own series, distinguished by Prometheus's `job` label, since health state is deliberately not shared, see §4 Failover), and `nimbus_nats_consumer_pending{consumer}` (worker polls its own JetStream consumer's `Info()` every 3s). Prometheus (`deploy/observability/prometheus.yml`) scrapes both processes every 5s; Grafana auto-provisions a datasource plus two dashboards from `deploy/observability/grafana/` on container start — no manual import step.

## 3. Deployment topology

**Local dev (Docker Compose) — built and running.** `deploy/docker-compose.yml`: `nimbus-api`, `nimbus-worker`, `postgres`, `redis`, `nats`, `minio-node-1/2/3` (standalone, not clustered), `prometheus`, `grafana`. This is the primary day-to-day loop for the backend.

**Frontend is *not* in Docker Compose yet** — runs via `cd frontend && npm run dev` against the Compose-hosted backend (`NEXT_PUBLIC_API_URL` in `frontend/.env.local`). Containerizing it (a `Dockerfile.web` + compose service, matching the pattern of `Dockerfile.api`/`Dockerfile.worker`) is a natural fold-in for Day 13, not yet done.

**Kubernetes (kind/minikube) — not started.** Planned scope (Day 13, unbuilt):
- Our own services (`nimbus-api`, `nimbus-worker`, and eventually the frontend) ship as a **Helm chart we own** — Deployments, Services, ConfigMap/Secret, HPA stub, liveness/readiness probes wired to §2.
- Stateful infra (Postgres, Redis, NATS, MinIO×3, Prometheus, Grafana) runs via lightweight manifests/StatefulSets in `deploy/k8s/infra/` — not hand-rolled Postgres HA, just enough to stand the dependency up in-cluster for a demo.
- This split is itself the talking point: "I wrote the Helm chart for the services I own; for infra I depend on, I used minimal manifests rather than reinventing StatefulSet operators that already exist as mature open-source charts."

## 4. Request lifecycle summary (ties §1 modules to System Design's sequence diagrams — matches the real code paths as of Day 10)

- **Upload**: frontend → `upload.CheckChunks` (dedup) → `upload.InitUpload` → `upload.InitChunk` (`storage.Resolve` + `Presign`) → direct client→MinIO PUT → `upload.CommitChunk` (cross-replica ETag check, persist via `file`/`storage` repos) → `upload.CompleteUpload` (`file.CreateWithVersion` or `AddVersion`) → best-effort `activity.RecordUpload` + NATS publish.
- **Download**: frontend → `file.DownloadPlan` resolves version's chunk list → `storage.Repository.LocationsForChunk` per chunk (recorded locations, not a fresh ring lookup) → `storage.Router.IsHealthy` orders healthy-first → direct client←MinIO GET, client-side fallback to the second URL on failure.
- **Share**: `sharing.CreateShare` persists token+expiry; unauthenticated `sharing.Resolve` re-enters the download lifecycle above via a public-safe `FileInfo` projection instead of a user session.
- **Failover**: `storage.HealthCheckLoop` (background goroutine, run independently by both `nimbus-api` and `nimbus-worker`) updates in-memory health state (+ Redis TTL keys for cross-process visibility); `storage.Resolve`/`IsHealthy` always read current in-memory state, so failover is "the next call just skips the dead node" — no separate failover code path to keep in sync.
- **Async processing**: `nimbus-worker` consumes `nimbus.uploads.completed` (durable JetStream consumer, max 5 redeliveries) → `processing.Processor.Process` reassembles chunks → generates a real thumbnail (images) or a placeholder (PDFs) → stores it → `file.SetThumbnailKey` → best-effort `activity.RecordThumbnail`.

## 5. Kubernetes scope decision

Resolved (was an open question in an earlier draft): our services get a real Helm chart; infra dependencies get minimal manifests, not fully-charted StatefulSets. Not yet implemented — Day 13.
