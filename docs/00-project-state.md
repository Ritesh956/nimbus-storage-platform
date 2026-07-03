# Project State — Nimbus Storage Platform

Status: current as of Day 14 (end of session)
This is the authoritative "what's actually true right now" snapshot. If any other doc in `docs/` disagrees with this one, this one wins — flag the drift and fix the other doc.

## What Nimbus is

A self-hosted, distributed cloud storage platform (Dropbox/Drive-alike) built as a portfolio-quality demonstration of distributed-systems engineering. Go modular-monolith backend + one extracted worker service, Next.js frontend, Docker Compose for local infra. Three deliberate "standout" features, all built and verified: consistent-hash-routed multi-node storage with replication/failover, chunked/resumable/deduplicated upload, event-driven NATS processing (thumbnails).

## Architecture (as built)

- **`nimbus-api`** (Go, `backend/cmd/api`): modular monolith. Domain modules under `backend/internal/`: `auth`, `org`, `folder`, `file`, `upload`, `storage`, `sharing`, `search`, `activity`. Cross-module boundaries enforced via small interfaces satisfied by adapters in `main.go` — no module reaches into another's Postgres tables.
- **`nimbus-worker`** (Go, `backend/cmd/worker`): separate binary, same `go.mod`, imports `internal/` packages as a library (not network calls). Subscribes to NATS JetStream, reassembles chunks, generates thumbnails, writes activity. Runs its own `storage.Router` + health-check loop (health state is in-process, not shared).
- **Frontend** (`frontend/`): Next.js 16.2.10, App Router, TypeScript, Tailwind v4, SWR, localStorage JWT storage. Runs via `npm run dev` for day-to-day dev; also containerized as of Day 13 (`deploy/Dockerfile.web`, standalone output) for Compose and Kubernetes.
- **Infra** (Docker Compose, `deploy/docker-compose.yml`): Postgres, Redis, NATS, 3× standalone MinIO nodes, `nimbus-api`, `nimbus-worker`, `nimbus-web`, `prometheus`, `grafana`. Both app processes expose `/metrics`; Prometheus scrapes both, Grafana auto-provisions its Prometheus datasource plus two dashboards from `deploy/observability/grafana/` on container start.
- **Kubernetes** (`deploy/k8s/`, Day 13): `helm/nimbus/` is the chart owned by this project (api/worker/web); `infra/` is plain manifests for stateful deps (Postgres/Redis/NATS/MinIO×3/Prometheus/Grafana). Deploys to a local `kind` cluster (`deploy/k8s/kind-config.yaml`) with NodePorts mapped to the same host ports Compose uses — the two aren't meant to run simultaneously.
- **Storage routing**: SHA-1 hash ring, 128 vnodes/node, Dynamo-style preference list, N=2 replication / W=2 write quorum, 2-state circuit breaker per node (closed/open — no half-open; the 2s health-check cadence already serves as the retry trial).
- **Upload**: client-side SHA-256 chunking (8 MiB default), presigned PUT direct to MinIO, dedup via `/chunks/check`, cross-replica ETag verification on commit.
- **Auth**: JWT HS256 (15 min) + rotating opaque refresh tokens (7 day, reuse-detection revokes the whole family), Redis-backed access-token blacklist for logout.

## Completed features (Days 1-10, all verified against a real running stack)

1. Repo scaffold, Docker Compose skeleton, config/logging/http platform packages, migrations, CI skeleton (lint+build only).
2. Auth (register/login/refresh/logout, rotation) + orgs/membership.
3. Folder + file metadata CRUD, trash/restore.
4. Storage module: hash ring, health checks, `/v1/admin/nodes`, failover.
5. Chunked/resumable/deduplicated upload.
6. Download-plan + version history/restore.
7. Public share links with ACL/expiry.
8. Full-text search (Postgres tsvector) + activity feed + NATS publish on upload-complete.
9. `nimbus-worker`: thumbnail generation (image + PDF placeholder), async activity writes.
10. Full Next.js frontend: auth, org/folder browser, drag-drop chunked upload with progress, file preview, sharing UI, trash UI, activity feed, admin node-health page. CORS added to the backend to support it.
11. Prometheus instrumentation on `nimbus-api` and `nimbus-worker` (`/metrics`, HTTP histogram by route pattern, upload throughput, storage placement failures, per-node health gauge, NATS consumer lag) + `prometheus`/`grafana` added to Compose + two auto-provisioned Grafana dashboards (golden signals, storage health). See docs/03-hld.md §2.
12. `scripts/chaos-node-kill.js`: full mid-upload chaos scenario (kill a node after 2 of 7 chunks commit, assert remaining placement avoids it, upload completes, download checksum-matches, node recovers) — 10/10 assertions pass on a real run. Targeted Go integration tests (`internal/auth` refresh-reuse-revokes-family, `internal/upload` concurrent-complete race) against real Postgres/Redis, gated behind `-tags=integration`. `scripts/load-upload.js` (k6): 60 concurrent VUs driving the real chunked-upload flow, 3467 uploads, 0% failures — proves NFR-2. See docs/07-distributed-architecture.md §5.
13. Frontend containerized (`deploy/Dockerfile.web`, Next.js standalone output) + added to Compose. `deploy/k8s/infra/` (plain manifests: Postgres/Redis/NATS/MinIO×3/Prometheus/Grafana) + `deploy/k8s/helm/nimbus/` (chart we own: api/worker/web, ConfigMap/Secret, HPA stub, migrate Job as a Helm pre-install hook). Deployed to a local `kind` cluster and verified live: migrations ran (16 tables), a full chunked upload completed via real presigned MinIO URLs reachable through kind's NodePort mappings, dedup worked, the worker/NATS thumbnail pipeline produced a `thumbnail_key`, and both Prometheus targets + both Grafana dashboards came up automatically. See docs/03-hld.md §3.
14. CI hardened: `frontend` job (npm lint+build) and `integration-test` job (real Postgres/Redis service containers, runs the `-tags=integration` suite) added; `docker-build` extended to all four images (api/worker/web/migrate). Chaos/load tests deliberately stay out of CI (scope decision, see "Known issues" below). README got the Mermaid architecture diagram and a numbered demo script, and now documents both the Compose and kind paths. See docs/09-roadmap.md Day 14.

Plus, inserted mid-plan (not in the original roadmap): repo restructured from a single tree into top-level `backend/` + `frontend/` siblings, at the user's request, ahead of Day 10.

## Known issues / gaps (real, not hypothetical — flag before treating any of this as done)

- **No rate limiting.** Designed (per-user Redis token bucket) but never built. A real gap if this ever took real traffic.
- **No DLQ remediation.** Failed NATS deliveries (after 5 retries) are documented as routing to a DLQ subject, but there's no consumer/alerting on it — a human would need to know to look.
- **`GET /v1/admin/orgs/{orgId}/usage`** was documented in early API drafts but never implemented. Not on any roadmap day yet.
- **`internal/admin` module never materialized** — the one admin read that exists (node health) lives directly in `storage.Handler`.
- **HPA is a stub.** `deploy/k8s/helm/nimbus/templates/hpa.yaml` exists and targets `nimbus-api`, but the kind demo cluster has no metrics-server installed, so `kubectl get hpa` shows `<unknown>` for CPU and it will never actually scale. Scaffold, not a wired autoscaling pipeline — matches docs/03-hld.md §3's framing.
- **`scripts/chaos-node-kill.js` and `scripts/load-upload.js` are deliberately not in CI** (decided explicitly with the user on Day 14, not an oversight) — both need real multi-container infra (Compose/kind) and real wall-clock time (~40s for the load test alone), a different cost/value trade-off than a per-push gate. They stay manual/local verification tools; `.github/workflows/ci.yml` runs vet/build/unit/integration/lint/docker-build only.
- **Helm chart isn't in CI either** — `docker-build` builds the four images but nothing runs `helm lint`/`helm template`/a `kind`-based smoke deploy in CI. A real gap if the chart drifts from what actually deploys; not yet on any roadmap day.
- **FR-4 (auth audit log: login/refresh/logout) doesn't exist.** `internal/activity` only ever records file/org events (`uploaded`, `thumbnail_generated`) — there's no equivalent for auth events. Found during a Day 14 SRS cross-check, not previously documented as a gap. Flag for the Day 15 DoD pass.
- **FR-8's "checksum verification ... on read" is client-facing only, not server-enforced.** `checksum_sha256` is stored at upload time and returned in API responses (`file.Handler`) so a client *can* verify it after downloading, but nothing on the server re-hashes reassembled/served bytes and rejects a mismatch on the read path. Upload-side verification is real (cross-replica ETag check in `upload.Service.CommitChunk`); read-side is not. Found during the same Day 14 cross-check.
- **Compose and kind can't run at the same time.** Both bind the same host ports (8080, 3000, 9000/9010/9020, 9090, 3001) by design (so the same URLs work in either environment) — a real constraint, not an oversight, but worth knowing before assuming both are up.

## Important design decisions (confirmed, deliberate, worth knowing before touching related code)

- **2-state circuit breaker, not 3-state with half-open** (`backend/internal/storage/breaker.go`) — the fixed 2s health-check cadence already acts as the half-open trial, so a third state added complexity without adding behavior. Don't "fix" this to 3-state without re-reading docs/04-lld.md §1.3 first.
- **Health state is read in-memory, never from Redis, on the hot path** (`Router.Resolve`/`IsHealthy`). Redis is write-only from the health loop's perspective, used only for cross-process visibility (a hypothetical second `nimbus-api` replica, or the admin view). Don't add a Redis read to the resolve path — that would reintroduce the network round-trip the design specifically avoids.
- **`activity` writes are mostly synchronous**, not purely worker-driven — e.g. `uploaded` is recorded directly in `upload.Service.CompleteUpload`. Only genuinely async, processing-derived events (`thumbnail_generated`) come from the worker. See docs/03-hld.md §1 revision note.
- **Standalone MinIO nodes, not MinIO distributed mode** — deliberate, because the replication/placement/failover logic being hand-rolled in `internal/storage` *is* the point of the project. Don't swap in MinIO's built-in clustering as a "simplification"; it would remove the thing being demonstrated.
- **CORS is wildcard (`NIMBUS_CORS_ORIGIN=*`) by design**, not an oversight — safe here because auth is Bearer-token, not cookie-based, so there's no ambient credential a wildcard origin could steal.
- **JWT is HS256, not RS256/JWKS** — fine for a single-service monolith; would need to change if `auth` is ever split into its own deployable.
- **`nimbus_storage_node_healthy` exists as two independent series, one per process** (`backend/internal/storage/router.go` probeOne), not a single shared gauge — a direct consequence of health state being deliberately in-process-only (see the Redis bullet above). `nimbus-api` and `nimbus-worker` each run their own probe loop and each report their own view; Prometheus's `job` label is what tells them apart on the storage-health dashboard. Don't "fix" this into one gauge — that would require introducing the shared-state read the design specifically avoids.
- **kind's NodePort mappings intentionally reuse Compose's host ports** (8080/3000/9000/9010/9020/9090/3001, see `deploy/k8s/kind-config.yaml`) rather than picking fresh ones — so `NIMBUS_STORAGE_NODES`' `PublicEndpoint` half, `NEXT_PUBLIC_API_URL`'s build-time default, and anything a person has bookmarked all stay valid regardless of which environment is currently up. The trade-off is the two environments can never run concurrently; considered a non-issue for a local demo, not an oversight.
- **The nimbus-migrate image runs migrations via a Helm pre-install/pre-upgrade hook Job**, not baked into `nimbus-api`'s startup — keeps schema migration a deploy-time, one-shot concern independent of how many `nimbus-api` replicas come up, matching how `docker-compose.yml`'s `migrate` service already works. The hook Job inlines its own Postgres DSN rather than reading the chart's `nimbus-config` ConfigMap, because pre-install hooks run before the chart's other resources exist.

## Current status

All 3 weeks of the original roadmap now have work landed: Days 1-10 (core product), 11 (Prometheus + Grafana), 12 (chaos/integration/load tests), 13 (frontend containerized, Kubernetes + Helm), and 14 (CI hardening, README diagram + demo script) are done. All design docs are up to date as of this session. Only Day 15 (buffer + final SRS Definition-of-Done pass) remains. See [docs/next-session.md](next-session.md) for the handoff.
