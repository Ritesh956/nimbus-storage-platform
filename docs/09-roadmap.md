# Implementation Roadmap — Nimbus Storage Platform

Status: DRAFT — final design doc before implementation starts
Version: 0.1
Depends on: all prior docs (01-08)

Ordered so every day ends with something runnable and testable — no long stretch where nothing works end-to-end. Within a day, features are still built and shown to you one at a time (per your original instruction: implement one piece, wait for approval, then continue) — this table is the sequence, not permission to batch a whole day into one drop.

## Week 1 — Foundations, core CRUD, distributed storage layer

| Day | Build | Deliverable / how it's checked | FRs |
|---|---|---|---|
| 1 | Repo scaffold, Docker Compose skeleton (postgres/redis/nats/minio×3 stubs), `platform/config`+`logging`+`httpserver`, migrations wired, CI skeleton (lint+build) | `docker compose up` → `GET /healthz` returns 200 | FR-31 |
| 2 | `auth` + `org` modules: register/login/refresh/logout, JWT+rotation, org+membership | curl script: register → login → create org → refresh token rotation demonstrated | FR-1..4 |
| 3 | `folder` + `file` metadata CRUD, trash/restore, full schema migrated | Folder tree built/moved/renamed/trashed/restored via API | FR-5, FR-11 |
| 4 | `storage` module: hash ring, health-check loop, Redis health table, `/v1/admin/nodes` | Stop a MinIO container → `/v1/admin/nodes` shows it `down` within 10s (NFR-3) | FR-17..20 |
| 5 | `upload` module: chunk check/init/commit/complete, checksum verify, dedup | Multi-chunk file uploaded end-to-end via test script, chunks confirmed on 2 nodes each | FR-6..9 |

**Checkpoint 1** (end of week 1): backend core + the distributed storage layer work and are independently testable via curl/scripts. This is the highest-risk, most load-bearing part of the project — pause here for your review before moving to product surface area.

## Week 2 — Product completeness + frontend

| Day | Build | Deliverable | FRs |
|---|---|---|---|
| 6 | Download-plan endpoint, version history + version restore | File uploaded, downloaded, re-uploaded as v2, restored to v1 — verified via checksum | FR-9, FR-10 |
| 7 | `sharing` module: public links, presigned access, basic ACL | Unauthenticated download via share link works; expired link rejected | FR-12..14 |
| 8 | `search` (tsvector) + `activity` + NATS publish on upload-complete | Search by name/type/date; activity event appears after upload | FR-15, FR-16, FR-22 |
| 9 | `nimbus-worker`: NATS consumer, thumbnail generation (image+PDF), activity write | Upload an image/PDF → thumbnail appears without any frontend involvement, via worker logs + storage check | FR-23, FR-24 |
| 10 | Next.js frontend: auth, folder browser, drag-drop chunked upload w/ progress, preview, sharing UI, trash UI, activity feed, admin node-health page | Full flow exercised in a real browser | FR-25, FR-26 |

**Checkpoint 2** (end of week 2): product is usable end-to-end in a browser by someone who isn't you. Pause for a real walkthrough before moving to ops/polish.

## Week 3 — Observability, chaos proof, deployment, polish

| Day | Build | Deliverable | FRs |
|---|---|---|---|
| 11 | Prometheus instrumentation (api/worker/storage), Grafana dashboards | Golden-signals dashboard + storage-health dashboard both populated under real traffic | FR-27..29 |
| 12 | `scripts/chaos-node-kill.sh`, integration test suite, k6 load script | Chaos script passes all assertions (Distributed Arch §5); load test hits NFR-2 (≥50 concurrent uploads) | FR-21, NFR-2, NFR-5 |
| 13 | K8s infra manifests + Helm chart for api/worker/web, deploy to kind | `helm install` on a local kind cluster, app reachable, probes green | FR-32 |
| 14 | CI hardening (lint/unit/integration/docker build on GitHub Actions), README (architecture diagram + demo script), rehearse the chaos demo | CI green on a fresh clone; a stranger can follow the README in <10 min (SRS DoD) | FR-30, FR-33 |
| 15 | Buffer: fix whatever broke in day 13-14, final pass against SRS §8 Definition of Done | Checklist complete | — |

## How we'll actually work day to day

- Each feature gets built, shown running (curl output, test result, or screenshot), and explicitly approved before the next one starts — same as your original instruction, just now sequenced.
- If something in week 1 turns out harder than scoped (most likely candidate: the storage router/failover), we slip inside week 1 rather than cut corners on it — it's the centerpiece. Frontend polish (day 10) and dashboard aesthetics (day 11) are the places to compress if time runs short, not the distributed storage layer or the chaos test.
- I'll flag any point where reality diverges from this plan rather than silently re-scoping.

## Ready to start

All 10 design-phase docs are written (`docs/01` through `docs/09`, plus this roadmap). Say go and I'll start Day 1: repo scaffold + Docker Compose skeleton + platform packages, and show you the running `/healthz` before touching anything else.
