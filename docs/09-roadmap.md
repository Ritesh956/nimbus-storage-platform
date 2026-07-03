# Implementation Roadmap — Nimbus Storage Platform

Status: current as of Day 14 — Days 1-14 complete, see docs/00-project-state.md for the authoritative status snapshot
Version: 0.6
Depends on: all prior docs (01-08)

Ordered so every day ends with something runnable and testable — no long stretch where nothing works end-to-end. Within a day, features are still built and shown one at a time, verified against a real running stack, before moving on.

## Week 1 — Foundations, core CRUD, distributed storage layer — ALL COMPLETE

| Day | Build | Deliverable / how it's checked | FRs | Status |
|---|---|---|---|---|
| 1 | Repo scaffold, Docker Compose skeleton (postgres/redis/nats/minio×3 stubs), `platform/config`+`logging`+`httpserver`, migrations wired, CI skeleton (lint+build) | `docker compose up` → `GET /healthz` returns 200 | FR-31 | **Done.** CI workflow itself (`.github/workflows/ci.yml`) was scoped as skeleton-only and never revisited — real gap, see docs/00-project-state.md. |
| 2 | `auth` + `org` modules: register/login/refresh/logout, JWT+rotation, org+membership | curl script: register → login → create org → refresh token rotation demonstrated | FR-1..4 | **Done.** |
| 3 | `folder` + `file` metadata CRUD, trash/restore, full schema migrated | Folder tree built/moved/renamed/trashed/restored via API | FR-5, FR-11 | **Done.** |
| 4 | `storage` module: hash ring, health-check loop, Redis health table, `/v1/admin/nodes` | Stop a MinIO container → `/v1/admin/nodes` shows it `down` within 10s (NFR-3) | FR-17..20 | **Done.** Breaker simplified to 2-state (closed/open) during build — see docs/04-lld.md §1.3. |
| 5 | `upload` module: chunk check/init/commit/complete, checksum verify, dedup | Multi-chunk file uploaded end-to-end via test script, chunks confirmed on 2 nodes each | FR-6..9 | **Done.** |

**Checkpoint 1**: passed. Distributed storage layer verified independently via `scripts/smoke-storage.sh` before moving to product surface area.

## Week 2 — Product completeness + frontend — ALL COMPLETE

| Day | Build | Deliverable | FRs | Status |
|---|---|---|---|---|
| 6 | Download-plan endpoint, version history + version restore | File uploaded, downloaded, re-uploaded as v2, restored to v1 — verified via checksum | FR-9, FR-10 | **Done.** |
| 7 | `sharing` module: public links, presigned access, basic ACL | Unauthenticated download via share link works; expired link rejected | FR-12..14 | **Done.** |
| 8 | `search` (tsvector) + `activity` + NATS publish on upload-complete | Search by name/type/date; activity event appears after upload | FR-15, FR-16, FR-22 | **Done.** Search tokenization bug found + fixed (migration 000004); `activity` revised to mostly-synchronous writes, not purely worker-driven — see docs/03-hld.md §1. |
| 9 | `nimbus-worker`: NATS consumer, thumbnail generation (image+PDF), activity write | Upload an image/PDF → thumbnail appears without any frontend involvement, via worker logs + storage check | FR-23, FR-24 | **Done.** DLQ consumer explicitly not built (documented gap, docs/07 §3). |
| — | *(inserted, not in original plan)* Restructure repo into `backend/` + `frontend/` top-level split | Requested mid-stream ahead of Day 10 to keep toolchains separate | — | **Done.** |
| 10 | Next.js frontend: auth, folder browser, drag-drop chunked upload w/ progress, preview, sharing UI, trash UI, activity feed, admin node-health page | Full flow exercised in a real browser (Claude Preview MCP) | FR-25, FR-26 | **Done.** CORS support added (not in original design, browser calls were blocked without it) — see docs/03-hld.md §2. Several real bugs found and fixed during live browser testing (render-time navigation, share-link UX, activity copy, layout clipping) — see docs/00-project-state.md "Known issues" for what's still outstanding vs. fixed. |

**Checkpoint 2**: passed. Product is usable end-to-end in a browser — verified via live Claude Preview MCP sessions (registration → org → upload → download → share → trash → activity), not just code review.

## Week 3 — Observability, chaos proof, deployment, polish — IN PROGRESS

| Day | Build | Deliverable | FRs | Status |
|---|---|---|---|---|
| 11 | Prometheus instrumentation (api/worker/storage), Grafana dashboards | Golden-signals dashboard + storage-health dashboard both populated under real traffic | FR-27..29 | **Done.** `/metrics` on both `nimbus-api` and `nimbus-worker`; HTTP histogram (route-pattern labeled), upload throughput, placement failures, per-node health gauge, NATS consumer lag (worker polls its own consumer `Info()`). Grafana datasource + both dashboards auto-provisioned via `deploy/observability/grafana/`. Verified against the real running stack: uploaded real files (smoke-upload.js, smoke-thumbnails.js) and confirmed counters moved, and stopped/restarted a live MinIO container to confirm `nimbus_storage_node_healthy` flips within the health TTL — see docs/03-hld.md §2. |
| 12 | `scripts/chaos-node-kill.js` (full mid-upload scenario), targeted Go integration tests, k6 load script | Chaos script passes all assertions (docs/07 §5); load test hits NFR-2 (≥50 concurrent uploads) | FR-21, NFR-2, NFR-5 | **Done.** Chaos script: 10/10 assertions pass on a real run (docs/07 §5 — built as `.js` not `.sh`, divergence explained there). Integration tests: `internal/auth` (refresh-token reuse revokes the whole family) and `internal/upload` (concurrent `/complete` — exactly one wins), gated behind `-tags=integration` against real Postgres/Redis. Load test: `scripts/load-upload.js` (k6) ramped to 60 concurrent VUs driving the real chunked-upload flow — 3467 uploads completed, 0% failures, all thresholds green. |
| 13 | K8s infra manifests + Helm chart for api/worker/web, deploy to kind | `helm install` on a local kind cluster, app reachable, probes green | FR-32 | **Done.** Frontend containerized (`Dockerfile.web`, standalone output, added to Compose too). `deploy/k8s/infra/` (Postgres/Redis/NATS/MinIO×3/Prometheus/Grafana, plain manifests) + `deploy/k8s/helm/nimbus/` (api/worker/web chart, migrate Job as a pre-install hook, HPA stub). Verified on a real kind cluster: migrations ran, a full chunked upload completed with real presigned MinIO URLs reachable from the host via kind's NodePort mappings, the worker/NATS thumbnail pipeline worked, both Prometheus targets and both Grafana dashboards came up. See docs/03-hld.md §3. |
| 14 | CI hardening (lint/unit/integration/docker build on GitHub Actions), README (architecture diagram + demo script), rehearse the chaos demo | CI green on a fresh clone; a stranger can follow the README in <10 min (SRS DoD) | FR-30, FR-33 | **Done.** CI now has 4 jobs: `build-test` (vet/build/unit, unchanged), `frontend` (new: lint+build), `integration-test` (new: real Postgres/Redis service containers, runs the `-tags=integration` suite), `docker-build` (extended to all four images: api/worker/web/migrate). `scripts/chaos-node-kill.js`/`load-upload.js` deliberately stay out of CI — a scope decision (need real multi-container infra + real wall-clock time), not an oversight. All new/changed job logic validated locally (`act` for `frontend`/`build-test`, throwaway Postgres/Redis containers for the integration-test steps) before relying on it. README got the Mermaid architecture diagram (reused from docs/02 §1) and a numbered demo script. |
| 15 | Buffer: fix whatever broke in day 13-14, final pass against SRS §8 Definition of Done | Checklist complete | — | **Not started.** |

## How we've actually worked, day to day (confirmed pattern, not aspirational)

- Each feature built, then verified against the real running stack (curl scripts, smoke scripts, or live browser sessions), not just code-reviewed — this caught several real bugs (search tokenization, CORS, nil-slice JSON, render-time navigation) that a read-through wouldn't have.
- Divergences from the original plan (2-state breaker, no rate limiting, no DLQ remediation, activity writes mostly synchronous, admin module never materialized) are flagged explicitly in docs rather than silently absorbed — see docs/00-project-state.md "Known issues / gaps" for the consolidated list.
- Frontend is not yet containerized; it runs via `npm run dev` against the Compose-hosted backend. Folding it into Compose is bundled with Day 13, not a separate task.

## Next up

Day 15: buffer / final SRS Definition-of-Done pass. See [docs/next-session.md](next-session.md) for the full handoff.
