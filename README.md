# Nimbus Storage Platform

A distributed cloud storage platform (Dropbox/Drive-alike), built to demonstrate real distributed-systems engineering: consistent-hash-routed storage nodes with replication and failover, chunked/resumable/deduplicated upload, and an event-driven processing pipeline — backing a modular-monolith Go API and a Next.js frontend.

Design docs (read in order): [docs/01-srs.md](docs/01-srs.md) · [02-system-design.md](docs/02-system-design.md) · [03-hld.md](docs/03-hld.md) · [04-lld.md](docs/04-lld.md) · [05-database-design.md](docs/05-database-design.md) · [06-api-design.md](docs/06-api-design.md) · [07-distributed-architecture.md](docs/07-distributed-architecture.md) · [08-folder-structure.md](docs/08-folder-structure.md) · [09-roadmap.md](docs/09-roadmap.md)

## Status

Weeks 1-2 of the [roadmap](docs/09-roadmap.md) (Days 1-10) are feature-complete: auth, orgs, folders, files, the distributed storage router (consistent hashing + failover), chunked/resumable/deduplicated upload, versioning + download plans, public sharing, search, activity feed, `nimbus-worker`'s async thumbnail pipeline, and a Next.js frontend covering all of it — verified live in a real browser.

Week 3 is complete:
- **Day 11** — Prometheus metrics on both `nimbus-api` and `nimbus-worker`, two auto-provisioned Grafana dashboards (golden signals; storage-node health, the one to watch during the chaos demo below).
- **Day 12** — a real mid-upload chaos test (`scripts/chaos-node-kill.js`: kill a storage node while a write is in flight, prove the upload still completes and the download still checksum-matches), targeted Go integration tests against real Postgres/Redis, and a k6 load test proving ≥50 concurrent uploads (NFR-2).
- **Day 13** — the frontend is now containerized, and the whole stack deploys to a local Kubernetes cluster (`kind`) via a Helm chart this project owns (`nimbus-api`/`nimbus-worker`/`nimbus-web`), with plain manifests for the infra it depends on (Postgres, Redis, NATS, MinIO, Prometheus, Grafana).
- **Day 14** — CI runs the full test pyramid on every push (backend vet/build/unit/integration, frontend lint/build, four Docker image builds) and this README got the architecture diagram and demo script below.
- **Day 15** (this, final day) — SRS Definition-of-Done pass: built the auth audit log (FR-4) and read-side checksum verification (FR-8), measured p95 metadata latency for the first time (NFR-4: 20.5ms, `scripts/load-metadata.js`), and turned on GitHub branch protection so CI passing is actually required before a merge (NFR-6), not just green.

See [docs/00-project-state.md](docs/00-project-state.md) for the authoritative, continuously-updated snapshot of what's built vs. designed-but-not-built.

## Architecture

```mermaid
flowchart TB
    subgraph Client
        WEB[Next.js Web App]
    end

    subgraph API[nimbus-api  (modular monolith)]
        AUTH[auth module]
        META[metadata module]
        ROUTE[storage-routing module\nconsistent hash ring + node health]
        SHARE[sharing module]
        SEARCH[search module]
    end

    WORKER[nimbus-worker\n(extracted service)]

    subgraph Storage["Storage nodes (standalone MinIO x3+)"]
        N1[(node-1)]
        N2[(node-2)]
        N3[(node-3)]
    end

    PG[(Postgres\nmetadata)]
    REDIS[(Redis\nsessions + node-health table + hash ring state)]
    NATS{{NATS\nevent bus}}
    PROM[Prometheus]
    GRAF[Grafana]

    WEB -- REST/JSON --> API
    WEB -- presigned PUT/GET --> Storage

    AUTH --> PG
    META --> PG
    SHARE --> PG
    SEARCH --> PG
    ROUTE --> REDIS
    ROUTE -- presigned URLs / health checks --> Storage

    API -- upload.completed event --> NATS
    NATS --> WORKER
    WORKER -- thumbnails, activity --> Storage
    WORKER --> PG

    API --> PROM
    WORKER --> PROM
    Storage --> PROM
    PROM --> GRAF
```

- **`nimbus-api`** — modular monolith (Go), one package per domain (`backend/internal/auth`, `backend/internal/folder`, `backend/internal/storage`, ...), hexagonal-style boundaries so any module can be extracted into its own service later.
- **`nimbus-worker`** — the one module extracted for real: consumes NATS events for async thumbnailing/activity-feed writes. Both binaries share the same `internal/` packages and each run their own storage-health probe loop (deliberately not shared — see [docs/04-lld.md](docs/04-lld.md) §5).
- **Storage layer** — 3+ standalone MinIO nodes behind Nimbus's own consistent-hashing router, with replication (N=2) and heartbeat-based failover. This is deliberately *not* MinIO's built-in distributed mode — see [docs/02-system-design.md §1](docs/02-system-design.md#1-component-diagram) for why.

Full rationale for every non-obvious decision (why a monolith, why standalone MinIO, why Postgres for metadata, the CAP trade-off in the write path, etc.) is in the design docs above — this project is meant to be defensible in an interview, not just working.

## Repo layout

- `backend/` — the Go module: `cmd/` (api, worker entrypoints), `internal/` (one package per domain), `migrations/`
- `frontend/` — Next.js/TypeScript/Tailwind web app, containerized (`deploy/Dockerfile.web`) as well as runnable via `npm run dev`
- `deploy/` — `docker-compose.yml` (full stack, backend + frontend + infra + observability) and `k8s/` (Kubernetes manifests + the Helm chart for `nimbus-api`/`nimbus-worker`/`nimbus-web`) — two deployment paths, see below
- `docs/` — the design docs (SRS through roadmap), read in order
- `scripts/` — smoke-test, chaos-test, and load-test scripts exercising the real running stack end to end

## Running locally

Two ways to run the full stack. **They use the same host ports on purpose** (8080 API, 3000 frontend, 9000/9010/9020 the three MinIO nodes, 9090 Prometheus, 3001 Grafana) so the same URLs work regardless of which one is up — which also means **don't run both at once**.

### Option A — Docker Compose (the day-to-day loop)

```sh
cp deploy/.env.example deploy/.env   # first time only
make dev                             # docker compose up --build (backend + infra + frontend)
```
Or, to iterate on the frontend outside its container: skip `nimbus-web` and run `cd frontend && npm install && npm run dev` in a separate terminal instead.

```sh
curl http://localhost:8080/healthz   # {"status":"ok"}
curl http://localhost:8080/readyz    # checks postgres/redis/nats connectivity
```
Frontend: http://localhost:3000 · Grafana: http://localhost:3001 (admin/nimbus) · Prometheus: http://localhost:9090

`make down` tears the backend stack down (including volumes).

### Option B — Kubernetes (kind + Helm)

Requires [`kind`](https://kind.sigs.k8s.io/) and [`helm`](https://helm.sh/) installed, and (if Compose is running) `docker compose -f deploy/docker-compose.yml down` first.

```sh
kind create cluster --name nimbus --config deploy/k8s/kind-config.yaml

docker build -f deploy/Dockerfile.api     -t nimbus-api:latest     .
docker build -f deploy/Dockerfile.worker  -t nimbus-worker:latest  .
docker build -f deploy/Dockerfile.web     -t nimbus-web:latest     .
docker build -f deploy/Dockerfile.migrate -t nimbus-migrate:latest .
kind load docker-image nimbus-api:latest nimbus-worker:latest nimbus-web:latest nimbus-migrate:latest --name nimbus

bash deploy/k8s/infra/apply.sh                          # Postgres/Redis/NATS/MinIO x3/Prometheus/Grafana
helm install nimbus deploy/k8s/helm/nimbus -n nimbus     # nimbus-api/worker/web + a migration Job (Helm pre-install hook)
```
Same URLs as Option A once both commands finish — `kubectl -n nimbus get pods` to watch rollout, `helm uninstall nimbus -n nimbus && kind delete cluster --name nimbus` to tear down.

## Demo script

A full walkthrough of the distributed-systems parts, in order:

1. **Register → org → folder** via the frontend (or `scripts/smoke-auth.sh` / `scripts/smoke-folders.sh` for the API-only version).
2. **Upload a multi-chunk file** (drag-drop in the UI, or `node scripts/smoke-upload.js`) — watch it get chunked client-side, hashed, and each chunk replicated to 2 of the 3 MinIO nodes. Upload the same file again to see dedup skip every chunk.
3. **Kill a node mid-upload**: `node scripts/chaos-node-kill.js` — starts a 7-chunk upload, `docker stop`s a MinIO node partway through, and asserts (10/10 checks on a real run): remaining chunks route around the dead node, the upload still completes, the download still checksum-matches, and the node recovers when restarted. Have the **`nimbus-storage-health` Grafana dashboard** open while this runs — the node-health panel visibly flips red then green within the ~6s detection window.
4. **Share it**: create a public share link, open it unauthenticated (`/shares/{token}`), confirm it downloads without a session.
5. **Check the thumbnail + activity feed**: `nimbus-worker` picked up the `upload.completed` event over NATS, generated a thumbnail asynchronously, and wrote a `thumbnail_generated` activity entry — both visible in the UI without any manual trigger.
6. **Load test** (optional, proves NFR-2): `docker run --rm --network host -e NIMBUS_BASE_URL=http://localhost:8080 -v "$(pwd)/scripts:/scripts" grafana/k6 run /scripts/load-upload.js` — ramps to 60 concurrent full upload sessions; last verified run: 3467 uploads, 0% failures.
7. **Metadata latency test** (optional, proves NFR-4): `docker run --rm --network host -e NIMBUS_BASE_URL=http://localhost:8080 -v "$(pwd)/scripts:/scripts" grafana/k6 run /scripts/load-metadata.js` — same 60-VU concurrency, hitting `GET /v1/orgs/{orgId}/folders` in isolation; last verified run: p95 20.5ms (bound is 200ms).

Steps 2, 4, 5, 6, and 7 work identically against the Compose stack or the `kind` deployment (Option A/B above) — the demo doesn't care which one is up. **Step 3 is Compose-only**: `chaos-node-kill.js` drives `docker compose stop`/`start` on a named container, which has no equivalent once MinIO runs as `kind` pods rather than Compose containers. The same failover *behavior* is demonstrable under `kind` (`kubectl delete pod -n nimbus minio-node-1-0` and watch the same dashboard), but the script itself isn't kind-portable — found during the Day 15 SRS DoD pass, see docs/00-project-state.md.
