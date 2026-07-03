# Nimbus Storage Platform

Distributed cloud storage platform (Dropbox/Drive-alike) — a portfolio-quality demonstration of distributed-systems engineering. Go backend (modular monolith + one extracted worker service), Next.js frontend, Docker Compose for local infra.

**Before doing anything else, read [docs/00-project-state.md](docs/00-project-state.md)** — it is the authoritative "what's actually true right now" snapshot (architecture, completed features, known gaps, confirmed design decisions). If any other doc disagrees with it, `docs/00-project-state.md` wins. For what to work on next, read [docs/next-session.md](docs/next-session.md).

Full design doc series lives in `docs/01` through `docs/09` (SRS → System Design → HLD → LLD → DB design → API design → distributed architecture → folder structure → roadmap).

## Repo layout

- `backend/` — Go module. `cmd/api` (nimbus-api) and `cmd/worker` (nimbus-worker) are thin entrypoints; all domain logic lives in `internal/` (hexagonal-style: `auth`, `org`, `folder`, `file`, `upload`, `storage`, `sharing`, `search`, `activity`, `platform/*`). See `docs/08-folder-structure.md`.
- `frontend/` — Next.js 16 (App Router, Turbopack), TypeScript, Tailwind v4. Runs via `npm run dev`, not containerized yet.
- `deploy/` — `docker-compose.yml` for backend + infra (Postgres, Redis, NATS, 3× MinIO). Frontend is not in Compose.
- `docs/` — design docs, kept in sync with the actual implementation, not aspirational.

## Running locally

```
docker compose -f deploy/docker-compose.yml up   # backend + infra
cd frontend && npm run dev                        # frontend, separate terminal
```

## Working conventions (read before making changes)

- **No module reaches into another's Postgres tables directly.** Cross-module reads/writes go through small interfaces (e.g. `FileCreator`, `MembershipChecker`) satisfied by adapters in `cmd/api/main.go`.
- **Verify against the real running stack, not just code review.** Backend: curl/smoke scripts in `scripts/`. Frontend: live browser testing (Claude Preview MCP tools) — this has caught real bugs (CORS, search tokenization, render-time navigation crashes) that code review alone missed.
- **When implementation diverges from a design doc, say so explicitly in the doc** rather than silently patching or leaving stale claims in place. `docs/00-project-state.md` "Known issues / gaps" is the running list — check it before assuming something documented is actually built (e.g. rate limiting, DLQ remediation, and the admin usage endpoint are designed but NOT implemented).
- Don't add abstractions, error handling, or config beyond what's asked — see the global engineering norms in the system prompt; this project follows them strictly (it's meant to read like real staff-level work, not a kitchen-sink demo).

## Known footguns

- MinIO's Go SDK does a *live* `GetBucketLocation` call during presigning unless `Region` is pinned in `minio.Options` (`backend/internal/storage/presign.go`) — don't remove that pin.
- Go's `nil` slice marshals to JSON `null`, not `[]` — watch for this on any handler returning a list (bit us once in `upload.Repository.FindMissingChunks`).
- Docker Desktop on this machine has previously left a stale Unix-socket reparse point after being killed/reinstalled. If `docker compose up` fails oddly on infra that previously worked, check for zombie `com.docker.*` processes before assuming a code problem.
- **This dev machine has a native Windows PostgreSQL 17 install competing with Docker Desktop for host port 5432.** `localhost:5432`/`127.0.0.1:5432`/`[::1]:5432` from the host can silently hit the native install instead of the compose container (wrong credentials, confusing "password authentication failed" errors that look like a code bug but aren't). The Go integration tests (`-tags=integration`, `internal/auth`/`internal/upload`) read `NIMBUS_TEST_POSTGRES_DSN`/`NIMBUS_TEST_REDIS_ADDR` env overrides for exactly this reason — when the host port is contended, run them inside a container on the `nimbus_default` compose network instead (`docker run --network nimbus_default -e NIMBUS_TEST_POSTGRES_DSN=postgres://nimbus:nimbus@postgres:5432/nimbus?sslmode=disable ...`).
- **Presigned MinIO URLs are signed against `PublicEndpoint` (`http://localhost:900x`), which only resolves correctly from whatever host Docker Compose actually publishes those ports to.** Any load/chaos tool that runs in its own container on the compose network (e.g. k6) can reach `nimbus-api` by service name fine, but the presigned PUT/GET URLs it gets back will fail with connection-refused, because "localhost" inside that container means the container itself. Fix: run the tool with `--network host` and hit `nimbus-api` via `localhost:8080` too (see `scripts/load-upload.js`'s header comment) — don't try to fix it by changing the URL host, that's what the server is correctly handing back for a real (non-containerized) client.
- No git remote is configured yet (local-only repo) — don't assume `git push` is expected without checking first.
