# High-Level Design — Nimbus Storage Platform

Status: DRAFT — defaults from System Design confirmed (8 MiB chunks, N=2/W=2, standalone-MinIO+custom routing)
Version: 0.1
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
| `activity` | reads events written by worker | `ListActivity(orgID)` | `PG` |
| `admin` | node/usage read model for the admin page | `NodeStatus()`, `OrgUsage()` | `storage`, `PG` |

`nimbus-worker` is a separate binary/module: subscribes to NATS, calls into shared `storage` and a trimmed `file`/`activity` write path via the same internal packages (imported as a library, not called over the network) — this is what keeps the "extract a service later" story honest: the worker already runs as its own process today.

## 2. Cross-cutting concerns

- **AuthN/Z**: JWT access token (15 min TTL) + refresh token (7 day TTL, rotated on use, old one blacklisted in Redis). Middleware injects `user_id`/`org_id` into request context; every handler checks org membership via `org.RequireRole()`.
- **Error model**: single JSON error envelope `{code, message, request_id}`; internal errors are wrapped with `%w` and mapped to a small fixed set of API codes (`not_found`, `conflict`, `invalid`, `unauthorized`, `internal`) at the HTTP boundary only — domain code never returns HTTP status codes.
- **Idempotency**: `chunk-commit` is naturally idempotent (keyed by content hash — re-committing the same hash is a no-op). `upload-complete` accepts a client-generated idempotency key so retried "complete" calls don't double-create a `FileVersion`.
- **Resilience toward storage nodes**: calls from `storage` module to a MinIO node use a short timeout (500ms) + a simple circuit breaker per node — if a node's breaker is open (recently failed), it's treated as unhealthy immediately rather than waiting on a timeout, which is what keeps failover fast (SRS NFR-3, ≤10s).
- **Rate limiting**: token bucket per user, stored in Redis (`INCR` + TTL), applied at the HTTP middleware layer ahead of auth-heavy and upload-init endpoints.
- **Config**: 12-factor — all config via environment variables (`NIMBUS_*`), validated at startup with fail-fast; no config files baked into images.
- **Secrets**: local dev via `.env` (gitignored) + Docker Compose env; Kubernetes via `Secret` objects mounted as env vars (not committed — see Helm chart values).
- **Correlation**: every inbound request gets/propagates an `X-Request-ID`; it's attached to the logger (via context) and forwarded on the NATS event so a log line in the worker can be traced back to the originating upload request.
- **Graceful shutdown**: SIGTERM drains in-flight requests (context cancellation with a bounded grace period) before exit — required for clean rolling restarts under Kubernetes.
- **Health/readiness**: `/healthz` (process alive) and `/readyz` (DB + Redis + NATS reachable) — wired to k8s liveness/readiness probes.

## 3. Deployment topology

**Local dev (Docker Compose)** — everything in one `docker-compose.yml`: `nimbus-api`, `nimbus-worker`, `web` (Next.js), `postgres`, `redis`, `nats`, `minio-node-1/2/3` (standalone, not clustered), `prometheus`, `grafana`. This is the primary day-to-day loop.

**Kubernetes (kind/minikube)** — scope kept deliberately narrower than "everything in k8s," to spend the time on the parts that demonstrate something:
- Our own services (`nimbus-api`, `nimbus-worker`, `web`) ship as a **Helm chart we own** — Deployments, Services, ConfigMap/Secret, HPA stub (even if not exercised under real load, it documents the intent), liveness/readiness probes wired to §2.
- Stateful infra (Postgres, Redis, NATS, MinIO×3, Prometheus, Grafana) runs via lightweight manifests/StatefulSets checked into `deploy/k8s/infra/` — not hand-rolled Postgres HA, just enough to stand the dependency up in-cluster for a demo.
- This split is itself the talking point: "I wrote the Helm chart for the services I own; for infra I depend on, I used minimal manifests rather than reinventing StatefulSet operators that already exist as mature open-source charts."

## 4. Request lifecycle summary (ties §1 modules to System Design's sequence diagrams)

- **Upload**: `web` → `upload.CheckChunks` (dedup) → `upload.InitChunk` → `storage.Resolve/Presign` → direct client→MinIO PUT → `upload.CommitChunk` (checksum verify, persist via `file`) → on full-file complete, publish NATS event.
- **Download**: `web` → `file` resolves version → chunk list → `storage.Resolve` per chunk (first healthy replica, second as client-side fallback) → direct client←MinIO GET.
- **Share**: `sharing.CreateLink` persists token+expiry; unauthenticated `sharing.ResolveLink` re-enters the download lifecycle above with link-scoped access instead of a user session.
- **Failover**: `storage.HealthCheckLoop` (background goroutine) updates Redis node-health table; `storage.Resolve` always reads current health, so failover is "the next `Resolve` call just skips the dead node" — no separate failover code path to keep in sync.

## 5. Open question for sign-off

K8s scope in §3 (our services get a real Helm chart, infra gets minimal manifests) — fine, or do you want infra fully charted too (more k8s surface area, less time for app features)?
