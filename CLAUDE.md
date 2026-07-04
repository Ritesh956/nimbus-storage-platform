# Nimbus Storage Platform

Distributed cloud storage platform (Dropbox/Drive-alike) — a portfolio-quality demonstration of distributed-systems engineering. Go backend (modular monolith + one extracted worker service), Next.js frontend, Docker Compose for local infra.

**Before doing anything else, read [docs/00-project-state.md](docs/00-project-state.md)** — it is the authoritative "what's actually true right now" snapshot (architecture, completed features, known gaps, confirmed design decisions). If any other doc disagrees with it, `docs/00-project-state.md` wins. For what to work on next, read [docs/next-session.md](docs/next-session.md) — it currently holds a **user-approved 15-item feature backlog** (agreed starting sequence: thumbnails-in-UI → members UI → upload caps/quotas → rate limiting → chunk GC); work through that rather than inventing new scope.

Full design doc series lives in `docs/01` through `docs/09` (SRS → System Design → HLD → LLD → DB design → API design → distributed architecture → folder structure → roadmap).

## Repo layout

- `backend/` — Go module. `cmd/api` (nimbus-api) and `cmd/worker` (nimbus-worker) are thin entrypoints; all domain logic lives in `internal/` (hexagonal-style: `auth`, `org`, `folder`, `file`, `upload`, `storage`, `sharing`, `search`, `activity`, `platform/*`). See `docs/08-folder-structure.md`.
- `frontend/` — Next.js 16 (App Router, Turbopack), TypeScript, Tailwind v4. Runs via `npm run dev` day-to-day; also containerized (`deploy/Dockerfile.web`) for Compose and Kubernetes.
- `deploy/` — `docker-compose.yml` (backend + infra + frontend) and `k8s/` (kind + Helm chart we own for api/worker/web, plain manifests for infra). See "Running locally" below — pick one, not both.
- `docs/` — design docs, kept in sync with the actual implementation, not aspirational.

## Running locally

Two ways to run the full stack — they use the same host ports (8080, 3000, 9000/9010/9020, 9090, 3001-or-3000) on purpose, so URLs/bookmarks/`NEXT_PUBLIC_API_URL` stay valid either way, but that also means **don't run both at once**.

```
# Docker Compose (day-to-day loop)
docker compose -f deploy/docker-compose.yml up
cd frontend && npm run dev   # optional: skip nimbus-web and iterate on the frontend directly instead

# Kubernetes (kind), from a stopped Compose stack
docker compose -f deploy/docker-compose.yml down
kind create cluster --name nimbus --config deploy/k8s/kind-config.yaml
docker build -f deploy/Dockerfile.api -t nimbus-api:latest . && docker build -f deploy/Dockerfile.worker -t nimbus-worker:latest . \
  && docker build -f deploy/Dockerfile.web -t nimbus-web:latest . && docker build -f deploy/Dockerfile.migrate -t nimbus-migrate:latest .
kind load docker-image nimbus-api:latest nimbus-worker:latest nimbus-web:latest nimbus-migrate:latest --name nimbus
bash deploy/k8s/infra/apply.sh
helm install nimbus deploy/k8s/helm/nimbus -n nimbus
```

**Tooling installed on this machine via `winget`** (Days 11-14, may not be on `PATH` in a fresh shell — see the footgun below): `gh`, `kind`, `helm`, `act`. Check `winget list` / `$env:LOCALAPPDATA\Microsoft\WinGet\Packages\...` before assuming any of these need installing again.

## Working conventions (read before making changes)

- **No module reaches into another's Postgres tables directly.** Cross-module reads/writes go through small interfaces (e.g. `FileCreator`, `MembershipChecker`) satisfied by adapters in `cmd/api/main.go`.
- **Verify against the real running stack, not just code review.** Backend: curl/smoke scripts in `scripts/`. Frontend: live browser testing (Claude Preview MCP tools) — this has caught real bugs (CORS, search tokenization, render-time navigation crashes) that code review alone missed. CI changes: `act` (installed via `winget install nektos.act`) runs `.github/workflows/ci.yml` jobs locally in Docker before trusting a workflow edit — see the footgun below for the flags it needs on this machine.
- **When implementation diverges from a design doc, say so explicitly in the doc** rather than silently patching or leaving stale claims in place. `docs/00-project-state.md` "Known issues / gaps" is the running list — check it before assuming something documented is actually built (e.g. rate limiting, DLQ remediation, and the admin usage endpoint are designed but NOT implemented).
- Don't add abstractions, error handling, or config beyond what's asked — see the global engineering norms in the system prompt; this project follows them strictly (it's meant to read like real staff-level work, not a kitchen-sink demo).

## Known footguns

- **`winget`-installed tools (`gh`, `kind`, `helm`, `act`) often aren't on `PATH` in a new shell** even though winget reports success — it modifies the registry PATH but an already-open or freshly-spawned shell may not pick it up. If `kind`/`helm`/`act`/`gh` report "command not found," don't reinstall — find the real path (`Get-ChildItem "$env:LOCALAPPDATA\Microsoft\WinGet\Packages" -Directory -Recurse -Filter "*.exe"`) and either add it to `PATH` for the session or invoke the full path directly.
- MinIO's Go SDK does a *live* `GetBucketLocation` call during presigning unless `Region` is pinned in `minio.Options` (`backend/internal/storage/presign.go`) — don't remove that pin.
- Go's `nil` slice marshals to JSON `null`, not `[]` — watch for this on any handler returning a list (bit us once in `upload.Repository.FindMissingChunks`).
- Docker Desktop on this machine has previously left a stale Unix-socket reparse point after being killed/reinstalled. If `docker compose up` fails oddly on infra that previously worked, check for zombie `com.docker.*` processes before assuming a code problem.
- **This dev machine has a native Windows PostgreSQL 17 install competing with Docker Desktop for host port 5432.** `localhost:5432`/`127.0.0.1:5432`/`[::1]:5432` from the host can silently hit the native install instead of the compose container (wrong credentials, confusing "password authentication failed" errors that look like a code bug but aren't). The Go integration tests (`-tags=integration`, `internal/auth`/`internal/upload`) read `NIMBUS_TEST_POSTGRES_DSN`/`NIMBUS_TEST_REDIS_ADDR` env overrides for exactly this reason — when the host port is contended, run them inside a container on the `nimbus_default` compose network instead (`docker run --network nimbus_default -e NIMBUS_TEST_POSTGRES_DSN=postgres://nimbus:nimbus@postgres:5432/nimbus?sslmode=disable ...`).
- **Presigned MinIO URLs are signed against `PublicEndpoint` (`http://localhost:900x`), which only resolves correctly from whatever host Docker Compose actually publishes those ports to.** Any load/chaos tool that runs in its own container on the compose network (e.g. k6) can reach `nimbus-api` by service name fine, but the presigned PUT/GET URLs it gets back will fail with connection-refused, because "localhost" inside that container means the container itself. Fix: run the tool with `--network host` and hit `nimbus-api` via `localhost:8080` too (see `scripts/load-upload.js`'s header comment) — don't try to fix it by changing the URL host, that's what the server is correctly handing back for a real (non-containerized) client.
- **Kubernetes' `command:` field overrides a container's image ENTRYPOINT; Compose's `command:` maps to CMD (args appended to the existing entrypoint).** Porting a Compose `command: server /data --console-address :9001` line (MinIO) straight into a k8s manifest as `command: [...]` breaks it — `exec: "server": executable file not found in $PATH`, because it replaced the image's real entrypoint (`minio`) instead of passing args to it. Use k8s `args:` for anything that was a Compose `command:`.
- **kubectl.exe (native Windows binary) invoked from Git Bash needs fully-resolved, Windows-style paths.** A literal `..` in a path passed to `kubectl -f`/`--from-file` (e.g. `$DIR/../observability/...`) fails with "system cannot find the path specified" even when the path is genuinely valid — resolve it first with a real `cd` (`cd "$DIR/../observability" && pwd`), and convert to Windows form with Git Bash's `pwd -W` before handing it to kubectl (see `deploy/k8s/infra/apply.sh`'s `to_win` helper). Plain POSIX-style (`/c/Users/...`) argument passing to kubectl.exe is unreliable enough not to depend on.
- **The root `.dockerignore` matters a lot on this repo** — without one, `docker build -f deploy/Dockerfile.*` with `context: ..` sends the *entire* repo as build context, including `frontend/node_modules` (hundreds of MB), turning a few-second context transfer into several minutes. Keep it excluding `**/node_modules`, `.git`, `frontend/.next`.
- **`frontend/package-lock.json` can pass `npm ci` on the Windows host but fail it inside a Linux/musl (`node:*-alpine`) build container** — optional platform-specific packages (`@tailwindcss/oxide-*`, `lightningcss-*`, `@unrs/resolver-binding-*`) resolve differently enough between platforms to trip npm's lockfile-sync check even though nothing about the actual dependency versions changed. `deploy/Dockerfile.web` uses `npm install`, not `npm ci`, for exactly this reason.
- **`act` needs an explicit runner image or it blocks on an interactive prompt that fails non-interactively** (`level=fatal msg="Incorrect function."`). Always pass `-P ubuntu-latest=catthehacker/ubuntu:act-latest`. If its own pull then fails with a bogus `authentication required` error, run `docker pull catthehacker/ubuntu:act-latest` directly first (works fine) and re-run `act` with `--pull=false` — something about `act`'s own pull invocation on this machine's Docker Desktop trips a credential-config issue that a plain `docker pull` doesn't.
- `.github/workflows/ci.yml`'s `integration-test` job applies migrations the same way local validation does: `docker run --network host -v <migrations>:/migrations migrate/migrate:v4.17.1 -path /migrations -database <dsn> up`. Validate changes to it against a throwaway Postgres/Redis on non-default host ports (e.g. `-p 15432:5432`), not `5432`/`6379` directly — same native-Postgres-port footgun as above.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private, set up Day 11). Commit and push at the end of each day/session's work, but confirm with the user before each push rather than doing it silently.
