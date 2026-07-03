# Handoff — Next Session

Written at the end of the session that completed Day 13 (frontend containerized, Kubernetes + Helm chart, deployed and verified on a real kind cluster). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective

**Day 14: CI hardening, README architecture diagram + demo script** (per [docs/09-roadmap.md](09-roadmap.md)).

Concretely, `.github/workflows/ci.yml` already has more than "skeleton-only" implies — check it before assuming it needs to be built from scratch. As of Day 13 it has: a `build-test` job (`go vet`, `go build`, `go test -short`, working-directory `backend`) and a `docker-build` job (builds `Dockerfile.api`/`Dockerfile.worker` images). What's still missing:
1. Frontend CI: `npm ci && npm run lint && npm run build` for `frontend/` (currently nothing runs against the frontend in CI at all).
2. Docker build coverage for the two images added Day 13: `Dockerfile.web` and `Dockerfile.migrate` (only api/worker are built today).
3. An integration-test stage running the `-tags=integration` Go tests (`internal/auth`, `internal/upload`) against real Postgres/Redis — GitHub Actions `services:` containers are the natural fit here, matching the "verify against the real thing" convention rather than mocking. Point `NIMBUS_TEST_POSTGRES_DSN`/`NIMBUS_TEST_REDIS_ADDR` at the service containers (see CLAUDE.md's footgun note on why these are configurable, not hardcoded).
4. Decide (with the user) whether `scripts/chaos-node-kill.js` and `scripts/load-upload.js` belong in CI at all — they need real multi-container infra (Compose or kind) and take real wall-clock time (the load test alone runs ~40s at 60 VUs), which is a different cost/value trade-off than unit/integration tests. A reasonable middle ground is a separate, manually-triggered or nightly workflow rather than blocking every PR — don't just wire them in without asking, this is a scope decision like Day 12's test-suite scope was.
5. README: architecture diagram (the docs/02 component diagram is the source to adapt) + a rehearsed demo script (register → org → chunked upload → see replication across nodes → `docker stop` a node mid-upload or run `scripts/chaos-node-kill.js` → still downloads → share link → thumbnail + activity feed). Mention both the Compose and kind paths, and point at the Grafana `nimbus-storage-health` dashboard as what should be on screen during the chaos moment. Also document the two deployment paths' port-sharing constraint (CLAUDE.md already has it; README doesn't yet).

## Remaining tasks after Day 14 (roadmap order)

- Day 15: buffer / final SRS Definition-of-Done pass (docs/01-srs.md §8) — go through every FR/NFR and confirm it's actually demoed, not just implemented; fix whatever Day 13/14 revealed as rough edges.

## What Day 13 actually built (context for anything that touches deployment)

- **Frontend containerization**: `deploy/Dockerfile.web` (multi-stage, `output: "standalone"` added to `frontend/next.config.ts`), `frontend/app/api/health/route.ts` (cheap liveness target, avoids a full page render on every probe). Added `nimbus-web` to `deploy/docker-compose.yml` — this pushed Grafana off host port 3000 onto 3001 (3000 is now the frontend's, matching `npm run dev` and the README).
- **`deploy/k8s/infra/`**: plain manifests for Postgres (StatefulSet), Redis/NATS (Deployments, unvolumed, matching Compose), and three independent MinIO StatefulSets (deliberately not one 3-replica StatefulSet — see CLAUDE.md/docs/00 on why standalone MinIO matters to this project). Prometheus/Grafana ConfigMaps are generated from `deploy/observability/` at apply time (`apply.sh`) rather than duplicated. Run via `bash deploy/k8s/infra/apply.sh` (waits for rollout before returning).
- **`deploy/k8s/helm/nimbus/`**: the chart we own — `nimbus-api`/`nimbus-worker`/`nimbus-web` Deployments+Services (api and web are NodePort; worker is ClusterIP, just for Prometheus scraping), a shared ConfigMap+Secret, an HPA stub on `nimbus-api` (no metrics-server in the demo cluster, so it's inert — a real known gap, not silently pretended-away, see docs/00), and a `nimbus-migrate` Job as a `helm.sh/hook: pre-install,pre-upgrade`. The migrate Job uses a new `deploy/Dockerfile.migrate` (`FROM migrate/migrate`, `COPY backend/migrations`) instead of duplicating the SQL into the chart.
- **`deploy/k8s/kind-config.yaml`**: NodePorts mapped to the *same* host ports Compose uses (8080/3000/9000/9010/9020/9090/3001) — this is why presigned MinIO URLs (`PublicEndpoint`) work unmodified from a kind deployment; see CLAUDE.md's footgun notes if this needs touching again.
- **Verified live, not just `helm template`**: `helm install` on a real kind cluster, migrations created all 16 tables, `scripts/smoke-upload.js` completed a full chunked upload (real presigned MinIO PUTs through the NodePort mappings) including a correctly-deduped second upload, `scripts/smoke-thumbnails.js`'s upload step confirmed the worker/NATS pipeline produces a `thumbnail_key` (checked via `kubectl exec postgres-0` since the script itself assumes Compose's container name), Prometheus shows both `nimbus-api`/`nimbus-worker` targets `up`, and Grafana auto-provisioned both dashboards.
- **Two real Kubernetes/Windows footguns hit and documented in CLAUDE.md**: Kubernetes' `command:` overriding the image ENTRYPOINT (Compose's `command:` doesn't — it's CMD/args), and kubectl.exe needing fully-resolved Windows-style paths when driven from Git Bash (a literal `..` in a path breaks it). Also hit and fixed: a missing root `.dockerignore` (825 MB build contexts), and a `frontend/package-lock.json` that passes `npm ci` on Windows but fails it in a Linux/musl build container (switched to `npm install` in `Dockerfile.web`).
- The kind cluster + Helm release were left running at the end of this session for inspection — check `kubectl get nodes`/`helm list -n nimbus` before assuming a clean slate, and note the Compose stack is currently down (can't run both — same host ports).

## Important context (carried forward, still true)

- **Verification pattern to keep using**: every feature gets built, then checked against the real running stack — Day 13 extended this to "and the real kind cluster," not just `helm template`/`kubectl apply --dry-run`.
- **Docs discipline**: when something diverges from the original plan, say so explicitly in the relevant doc. This session found and fixed a real one from Day 12 that had been missed: `docs/08-folder-structure.md` still said `chaos-node-kill.sh` (unbuilt) instead of the `.js` file that actually shipped — now corrected, and worth a quick glance at other docs for similar staleness before assuming they're current.
- **Known real gaps** (not yet built, don't assume otherwise): no rate limiting, no DLQ consumer, no `admin` module, HPA stub inert (no metrics-server), CI doesn't run integration/chaos/load tests or build web/migrate images yet. Full list in docs/00-project-state.md.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432 — doesn't affect the app (talks to Postgres via service/container DNS names, never `localhost`), but bites anything run from the host that hardcodes `localhost:5432`.
- `kind`/`helm` were installed this session via `winget` but aren't reliably on `PATH` in fresh shells on this machine — if a command isn't found, check `winget list` and fall back to the full path under `$env:LOCALAPPDATA\Microsoft\WinGet\Packages\...` rather than assuming the tool isn't installed.
- A git remote *is* configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private) as of Day 11 — commits get pushed at the end of each day/session, confirmed with the user first. Don't assume this is still local-only.
