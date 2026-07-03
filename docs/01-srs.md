# Software Requirements Specification — Nimbus Storage Platform

Status: DRAFT — pending sign-off
Version: 0.1
Scope window: 2-3 week solo build

> Working name "Nimbus" — trivial to rename later (`s/nimbus/<name>/` across the repo). Say the word if you want something else before we scaffold code.

## 1. Purpose

Nimbus is a self-hosted, distributed cloud storage platform (Dropbox/Drive-alike) built to demonstrate real distributed-systems engineering — not a feature checklist. The system prioritizes a small number of things done correctly under failure over a large number of things done superficially.

## 2. Architecture decision (binding for this SRS)

**Modular monolith, not microservices**, with one Go service (`nimbus-api`) internally organized by domain (`internal/auth`, `internal/metadata`, `internal/storage`, `internal/sharing`, `internal/search`) behind clean interfaces — hexagonal style, so any module can be extracted into its own deployable later.

One module *will* be extracted for real: the async processing worker (thumbnailing, activity feed) consuming NATS events, to prove the seam actually works, not just that it exists on paper.

The storage layer itself is the one deliberately distributed subsystem: multiple MinIO-backed storage nodes addressed via consistent hashing, with replication and failure detection. This is where "distributed systems" claims get backed by working code.

## 3. In-scope functional requirements

### 3.1 Identity & access
- FR-1 User registration + login (email/password)
- FR-2 JWT access tokens + refresh token rotation
- FR-3 Organizations (one owner) and membership (owner/member roles only — no fine-grained RBAC in v1)
- FR-4 Basic audit log of auth events (login, token refresh, logout)

### 3.2 Files & folders
- FR-5 Nested folders, create/rename/move/delete
- FR-6 File upload: chunked, resumable, multipart, client-parallelizable
- FR-7 Content-addressable chunk hashing (SHA-256) with dedup across users at the chunk level
- FR-8 Checksum verification on upload and on read (integrity check)
- FR-9 File download, including streamed/ranged download for large files
- FR-10 Version history per file; restore a prior version
- FR-11 Trash (soft delete) with restore; permanent delete after retention window or explicit purge

### 3.3 Sharing
- FR-12 Public share links with optional expiry
- FR-13 Presigned URLs for direct-to-storage-node upload/download
- FR-14 Basic ACL: private / org-visible / public-link

### 3.4 Search & metadata
- FR-15 Metadata indexed in Postgres (name, path, size, mime type, owner, timestamps)
- FR-16 Search by name/path/type/owner; simple filters (date range, size range)

### 3.5 Distributed storage layer
- FR-17 ≥3 storage nodes (MinIO instances) behind a consistent-hashing ring
- FR-18 Replication factor N=2 across nodes for every chunk
- FR-19 Heartbeat/health-check between the storage-routing layer and nodes
- FR-20 Automatic failover: a dead node is detected and excluded from routing within a bounded time; reads/writes continue against remaining replicas
- FR-21 A scripted, repeatable chaos scenario: kill a node mid-upload/download and demonstrate correct behavior (this is a deliverable, not just a design claim)

### 3.6 Async processing
- FR-22 Upload completion emits a NATS event
- FR-23 A separate worker process consumes events to generate image/PDF thumbnails and write activity-feed entries
- FR-24 Activity feed visible per-folder/per-org

### 3.7 Frontend
- FR-25 Next.js + TypeScript + Tailwind web client: auth, folder browser, drag-drop upload with progress, file preview (image/PDF), sharing UI, version history UI, trash UI, activity feed
- FR-26 Minimal admin view (storage node health, per-org usage) — a page, not a separate service

### 3.8 Observability & ops
- FR-27 Structured logging (JSON) across the API and worker
- FR-28 Prometheus metrics (request latency/rate/error, upload throughput, storage node health, queue depth)
- FR-29 Grafana dashboard(s) built on those metrics
- FR-30 GitHub Actions CI: lint, unit tests, build, docker image build on push

### 3.9 Deployment
- FR-31 Docker Compose for local dev (api, worker, postgres, redis, nats, 3x minio, prometheus, grafana)
- FR-32 Kubernetes manifests + one Helm chart, deployable to kind/minikube locally
- FR-33 README with architecture diagram and a scripted demo walkthrough (including the chaos scenario)

## 4. Explicit non-goals (v1)

These are named so nobody — including future us — mistakes their absence for an oversight:

- Desktop sync client / filesystem watcher / offline sync protocol
- CLI client
- Standalone leader-election algorithm (Raft/etcd) as a general primitive — node liveness uses heartbeat + health-check only, not full consensus
- Full chaos-engineering suite — one scripted, reliable scenario, not a framework
- Terraform / real cloud deployment / CDN
- Fine-grained RBAC, multi-team org structure
- gRPC (REST only — gRPC+gateway adds real complexity for no v1 payoff; noted as a documented roadmap item)
- OpenTelemetry distributed tracing, Loki log aggregation — structured logs + Prometheus/Grafana only
- Compression, encryption-at-rest as a configurable pipeline (checksumming yes; encryption noted as roadmap, see §6)
- Storage rebalancing / background compaction / GC as automated jobs (dedup GC may be a manual/documented process, not a scheduled system)
- Billing, quotas enforcement beyond a soft usage counter

## 5. Non-functional requirements (demo-scale, honestly stated)

Targets are sized to what will actually be run and demonstrated locally, not aspirational internet-scale numbers — with a companion "how this would scale" discussion in the System Design doc for the numbers in the original brief (10M users / 100PB).

- NFR-1 Handle files up to 5 GB via chunked upload on a laptop-class dev machine
- NFR-2 Demonstrate ≥50 concurrent uploads without data corruption (load-tested, not simulated)
- NFR-3 Storage node failure detected and routed around within 10s (heartbeat interval based)
- NFR-4 p95 API latency < 200ms for metadata operations under the above load
- NFR-5 All integrity checks (checksums) must catch injected corruption in the chaos test
- NFR-6 CI must pass (lint + unit + integration against docker-compose services) before merge

## 6. Roadmap (post-v1, not built now)

Desktop/CLI sync, real RBAC, gRPC API, OTel tracing + Loki, Terraform + real cloud deploy, encryption-at-rest, automated rebalancing/compaction/GC, chaos-engineering framework, quotas/billing enforcement.

## 7. Assumptions & constraints

- Single developer, ~2-3 weeks elapsed time
- All infra runs locally (Docker Compose / kind); no cloud spend
- Go for backend/worker, Next.js/TypeScript/Tailwind for frontend, Postgres for metadata, Redis for cache/session, MinIO for object storage, NATS for eventing — as originally specified
- "Production-grade" is interpreted as *correct, tested, and honestly documented*, not *literally ready for 10M users* — the SRS explicitly separates what's built from what's designed-for-but-not-built

## 8. Definition of done for v1

Every FR in §3 implemented and demoed; the chaos scenario (FR-21) runs reproducibly via a documented script; CI green; README walkthrough lets a stranger clone, `docker compose up`, and exercise the full flow (register → upload chunked file → see it replicated across nodes → kill a node → still download → share a link → see thumbnail + activity feed) in under 10 minutes.
