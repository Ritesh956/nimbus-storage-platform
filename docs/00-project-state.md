# Project State — Nimbus Storage Platform

Status: current as of Day 10 (end of session)
This is the authoritative "what's actually true right now" snapshot. If any other doc in `docs/` disagrees with this one, this one wins — flag the drift and fix the other doc.

## What Nimbus is

A self-hosted, distributed cloud storage platform (Dropbox/Drive-alike) built as a portfolio-quality demonstration of distributed-systems engineering. Go modular-monolith backend + one extracted worker service, Next.js frontend, Docker Compose for local infra. Three deliberate "standout" features, all built and verified: consistent-hash-routed multi-node storage with replication/failover, chunked/resumable/deduplicated upload, event-driven NATS processing (thumbnails).

## Architecture (as built)

- **`nimbus-api`** (Go, `backend/cmd/api`): modular monolith. Domain modules under `backend/internal/`: `auth`, `org`, `folder`, `file`, `upload`, `storage`, `sharing`, `search`, `activity`. Cross-module boundaries enforced via small interfaces satisfied by adapters in `main.go` — no module reaches into another's Postgres tables.
- **`nimbus-worker`** (Go, `backend/cmd/worker`): separate binary, same `go.mod`, imports `internal/` packages as a library (not network calls). Subscribes to NATS JetStream, reassembles chunks, generates thumbnails, writes activity. Runs its own `storage.Router` + health-check loop (health state is in-process, not shared).
- **Frontend** (`frontend/`): Next.js 16.2.10, App Router, TypeScript, Tailwind v4, SWR, localStorage JWT storage. Runs via `npm run dev`, **not containerized**.
- **Infra** (Docker Compose, `deploy/docker-compose.yml`): Postgres, Redis, NATS, 3× standalone MinIO nodes, `nimbus-api`, `nimbus-worker`. Prometheus/Grafana containers are declared in the design docs' target diagram but **not yet added to compose or instrumented** — Day 11 work.
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

Plus, inserted mid-plan (not in the original roadmap): repo restructured from a single tree into top-level `backend/` + `frontend/` siblings, at the user's request, ahead of Day 10.

## Known issues / gaps (real, not hypothetical — flag before treating any of this as done)

- **No metrics/observability yet.** Prometheus/Grafana appear in design docs as target state only; nothing is instrumented. Day 11.
- **No rate limiting.** Designed (per-user Redis token bucket) but never built. A real gap if this ever took real traffic.
- **No DLQ remediation.** Failed NATS deliveries (after 5 retries) are documented as routing to a DLQ subject, but there's no consumer/alerting on it — a human would need to know to look.
- **`GET /v1/admin/orgs/{orgId}/usage`** was documented in early API drafts but never implemented. Not on any roadmap day yet.
- **`internal/admin` module never materialized** — the one admin read that exists (node health) lives directly in `storage.Handler`.
- **Frontend isn't containerized.** No `Dockerfile.web`, not in `docker-compose.yml`. Runs via `npm run dev` against the Compose-hosted backend.
- **No Kubernetes/Helm yet.** `deploy/k8s/` doesn't exist. Day 13, per roadmap.
- **CI is skeleton-only** (lint+build). No unit/integration test stage, no Docker build stage, despite `docs/08-folder-structure.md`'s target layout describing more. Day 14.
- **Chaos test is partial.** `scripts/smoke-storage.sh` (Day 4) proves node-down detection/recovery at rest. It does **not** prove an in-flight upload survives a node dying mid-write — that's the still-unbuilt `scripts/chaos-node-kill.sh`, planned Day 12. See docs/07-distributed-architecture.md §5.
- **README** has local-run instructions but no architecture diagram or rehearsed demo script yet (Day 14 target).

## Important design decisions (confirmed, deliberate, worth knowing before touching related code)

- **2-state circuit breaker, not 3-state with half-open** (`backend/internal/storage/breaker.go`) — the fixed 2s health-check cadence already acts as the half-open trial, so a third state added complexity without adding behavior. Don't "fix" this to 3-state without re-reading docs/04-lld.md §1.3 first.
- **Health state is read in-memory, never from Redis, on the hot path** (`Router.Resolve`/`IsHealthy`). Redis is write-only from the health loop's perspective, used only for cross-process visibility (a hypothetical second `nimbus-api` replica, or the admin view). Don't add a Redis read to the resolve path — that would reintroduce the network round-trip the design specifically avoids.
- **`activity` writes are mostly synchronous**, not purely worker-driven — e.g. `uploaded` is recorded directly in `upload.Service.CompleteUpload`. Only genuinely async, processing-derived events (`thumbnail_generated`) come from the worker. See docs/03-hld.md §1 revision note.
- **Standalone MinIO nodes, not MinIO distributed mode** — deliberate, because the replication/placement/failover logic being hand-rolled in `internal/storage` *is* the point of the project. Don't swap in MinIO's built-in clustering as a "simplification"; it would remove the thing being demonstrated.
- **CORS is wildcard (`NIMBUS_CORS_ORIGIN=*`) by design**, not an oversight — safe here because auth is Bearer-token, not cookie-based, so there's no ambient credential a wildcard origin could steal.
- **JWT is HS256, not RS256/JWKS** — fine for a single-service monolith; would need to change if `auth` is ever split into its own deployable.

## Current status

Weeks 1 and 2 of the 3-week roadmap are complete (Days 1-10). All ten design docs are up to date as of this session. Week 3 (observability, full chaos test, Kubernetes, CI hardening, final polish) has not started. Next objective is Day 11. See [docs/next-session.md](next-session.md) for the handoff.
