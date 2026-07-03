# Nimbus Storage Platform

A distributed cloud storage platform (Dropbox/Drive-alike), built to demonstrate real distributed-systems engineering: consistent-hash-routed storage nodes with replication and failover, chunked/resumable/deduplicated upload, and an event-driven processing pipeline — backing a modular-monolith Go API and a Next.js frontend.

Design docs (read in order): [docs/01-srs.md](docs/01-srs.md) · [02-system-design.md](docs/02-system-design.md) · [03-hld.md](docs/03-hld.md) · [04-lld.md](docs/04-lld.md) · [05-database-design.md](docs/05-database-design.md) · [06-api-design.md](docs/06-api-design.md) · [07-distributed-architecture.md](docs/07-distributed-architecture.md) · [08-folder-structure.md](docs/08-folder-structure.md) · [09-roadmap.md](docs/09-roadmap.md)

## Status

Backend (weeks 1-2 of the [roadmap](docs/09-roadmap.md)) is feature-complete: auth, orgs, folders, files, the distributed storage router (consistent hashing + failover), chunked/resumable/deduplicated upload, versioning + download plans, public sharing, search, activity feed, and `nimbus-worker`'s async thumbnail pipeline. Frontend (Next.js, `frontend/`) starts Day 10.

## Repo layout

- `backend/` — the Go module: `cmd/` (api, worker entrypoints), `internal/` (one package per domain), `migrations/`
- `frontend/` — Next.js/TypeScript/Tailwind web app (Day 10+)
- `deploy/` — Docker Compose stack + Dockerfiles + observability config, orchestrating both
- `docs/` — the design docs (SRS through roadmap), read in order
- `scripts/` — smoke-test scripts exercising the real running stack end to end

## Running locally

```sh
cp deploy/.env.example deploy/.env   # first time only
make dev                             # docker compose up --build
```

Then:
```sh
curl http://localhost:8080/healthz   # {"status":"ok"}
curl http://localhost:8080/readyz    # checks postgres/redis/nats connectivity
```

`make down` tears the stack down (including volumes).

## Architecture at a glance

- **`nimbus-api`** — modular monolith (Go), one package per domain (`backend/internal/auth`, `backend/internal/folder`, `backend/internal/storage`, ...), hexagonal-style boundaries so any module can be extracted into its own service later.
- **`nimbus-worker`** — the one module extracted for real: consumes NATS events for async thumbnailing/activity-feed writes.
- **Storage layer** — 3+ standalone MinIO nodes behind Nimbus's own consistent-hashing router, with replication (N=2) and heartbeat-based failover. This is deliberately *not* MinIO's built-in distributed mode — see [docs/02-system-design.md §1](docs/02-system-design.md#1-component-diagram) for why.

Full rationale for every non-obvious decision (why a monolith, why standalone MinIO, why Postgres for metadata, the CAP trade-off in the write path, etc.) is in the design docs above — this project is meant to be defensible in an interview, not just working.
