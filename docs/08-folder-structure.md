# Project Folder Structure — Nimbus Storage Platform

Status: current as of Day 10
Version: 0.3
Depends on: [03-hld.md](03-hld.md) §1 (module matrix), [03-hld.md](03-hld.md) §3 (deployment topology)

Single monorepo, split into `backend/` (Go module) and `frontend/` (Next.js app) at the top level — split repos would add checkout/versioning overhead with no independent-release benefit at this project's size, but a clean top-level split still keeps each toolchain's config (go.mod, package.json, linters) from tangling with the other's, and makes it obvious at a glance which files a backend-only or frontend-only change should touch.

```
nimbus/
├── docs/                              # phase-by-phase design docs (this series)
│
├── backend/                           # the Go module — everything below is one `go build ./...`
│   ├── cmd/                           # thin entrypoints only — flag/env parsing, wiring, then delegate to internal/
│   │   ├── api/
│   │   │   └── main.go                # builds nimbus-api: http server + all domain modules
│   │   └── worker/
│   │       └── main.go                # builds nimbus-worker: NATS consumer + processing pipeline
│   │
│   ├── internal/                      # not importable outside this module — enforces "no one reaches into our packages"
│   │   ├── auth/                      # users, sessions, JWT/refresh (LLD §3)
│   │   ├── org/                       # organizations, membership, role checks
│   │   ├── folder/                    # folder tree CRUD, move/rename, cascade trash/restore
│   │   ├── file/                      # files, versions, download-plan, trash/restore/purge
│   │   ├── upload/                    # chunk state machine (LLD §2), dedup check, complete
│   │   ├── storage/                   # the distributed piece: ring.go, router.go, breaker.go, presign.go, fetch.go (LLD §1)
│   │   ├── sharing/                   # public share links, ACL resolution
│   │   ├── search/                    # metadata query against Postgres tsvector
│   │   ├── activity/                  # org activity feed (read + synchronous/worker writes)
│   │   ├── processing/                # nimbus-worker's domain logic: reassemble chunks, generate thumbnails
│   │   ├── events/                    # NATS JetStream publish/subscribe helpers, shared by api + worker
│   │   ├── admin/                     # reserved: currently empty — node/usage admin views live in storage.Handler for now
│   │   └── platform/                  # cross-cutting, no domain logic
│   │       ├── config/                # env parsing + validation (HLD §2)
│   │       ├── logging/               # structured logger, request-ID propagation
│   │       ├── httpserver/            # middleware chain assembly (LLD §4), health/ready handlers, error envelope
│   │       ├── db/                    # postgres/redis/nats client construction
│   │       └── idgen/                 # client-side UUIDv4 (e.g. refresh-token family_id before insert)
│   │
│   ├── migrations/                    # golang-migrate SQL files, numbered; matches docs/05-database-design.md
│   ├── test/                          # integration/load tests needing real infra (see Notes)
│   └── go.mod / go.sum
│
├── frontend/                          # Next.js 16 (App Router, Turbopack) + TypeScript + Tailwind v4 — built Day 10
│   ├── app/                           # routes: /, /login, /register, /app, /app/org/[orgId]/{,folder/[folderId],search,trash,activity,admin}, /shares/[token]
│   ├── components/                    # AppShell, RequireAuth, UploadDropzone, FileRow, ui/{Button,Card,Input,Badge}
│   ├── lib/                           # api.ts (typed client, auto-refresh-on-401), upload.ts (chunk/hash/dedup orchestration), auth-context.tsx, tokens.ts, types.ts, format.ts
│   ├── .env.local                     # NEXT_PUBLIC_API_URL — gitignored
│   └── package.json / tsconfig.json / next.config.ts
│
├── deploy/                            # orchestrates both backend/ and frontend/ — lives at the top level, not inside either
│   ├── docker-compose.yml             # full local stack (HLD §3)
│   ├── Dockerfile.api / Dockerfile.worker   # context = repo root, COPY backend/... (see compose build config)
│   ├── k8s/
│   │   ├── infra/                     # minimal manifests: postgres, redis, nats, minio x3, prometheus, grafana
│   │   └── helm/nimbus/               # chart we own: api, worker, web (Deployment/Service/ConfigMap/Secret/HPA stub)
│   └── observability/
│       ├── prometheus.yml
│       └── grafana/dashboards/        # golden-signals.json, storage-health.json (the chaos-demo dashboard)
│
├── scripts/                           # smoke-test scripts exercising the real running stack end to end
│   ├── smoke-*.sh / smoke-*.js        # one per day's deliverable built so far (auth, folders, storage, upload, versions, sharing, search/activity, thumbnails)
│   └── chaos-node-kill.sh             # NOT YET BUILT — planned Day 12, the fuller FR-21 scenario (kill-mid-upload); see docs/07 §5
│
├── .github/workflows/ci.yml           # lint, unit test, docker build (FR-30) — working-directory: backend for Go steps
├── Makefile                           # make dev / make build / make test / make lint
└── README.md                          # architecture overview + local run instructions (FR-33)
```

## Notes on non-obvious choices

- **`backend/` and `frontend/` as top-level siblings, not `web/` alongside `cmd/`/`internal/`**: the original single-tree layout worked fine through the backend-only weeks, but once a second toolchain (Node/Next.js) joins, keeping Go's module root uncluttered by `frontend/`'s `node_modules/`/`package.json`/etc. (and vice versa) makes both easier to reason about, lint, and containerize independently. `deploy/` stays at the top level since it orchestrates both.
- **`internal/` for everything backend, no `pkg/`**: nothing here is meant to be imported by another Go module — there's no "public library" audience.
- **Unit tests live beside the code they test** (`internal/storage/ring_test.go`, standard Go convention); `backend/test/` is only for tests that need real infrastructure running (integration, load) — the split matters because CI runs them as separate stages.
- **`cmd/api` and `cmd/worker` share all `internal/` packages** — this is what makes the "worker is already extracted as its own process" claim (HLD §1) concrete: they're two binaries built from one module, not one binary with a mode flag. Both independently construct their own `storage.Router` (each needs its own in-process health view — see LLD §5).
- **Migrations are plain numbered SQL, not an ORM's auto-migration** — the schema in docs/05 is hand-designed (indexes, partial unique constraints, generated columns); an ORM's migration generator would fight some of those choices. Four migrations exist as of Day 9: initial schema, uploads/upload_chunks, upload target_file_id, and a search-tokenization fix (all documented in docs/05 and in each migration's own comment).
- **`internal/admin` is a reserved, currently-empty package** — the admin-facing reads that exist today (storage node health) live directly in `storage.Handler` since they're a thin read over that module's own data; a real `admin` module would earn its keep once org-usage views or cross-module admin actions land.
- **`deploy/`, `.github/workflows/ci.yml`, and the Kubernetes/Helm layout under `deploy/k8s/` are still as originally planned, not yet built** — Docker Compose (backend + infra only, not frontend) is the only deployment path that exists today; see docs/03-hld.md §3.
