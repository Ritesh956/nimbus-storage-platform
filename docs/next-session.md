# Handoff — Next Session

Written at the end of the session that completed Day 12 (chaos test, integration tests, k6 load test). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective

**Day 13: Kubernetes manifests + Helm chart, deploy to kind; containerize the frontend** (per [docs/09-roadmap.md](09-roadmap.md), docs/03-hld.md §3/§5).

Concretely:
1. Stateful infra (Postgres, Redis, NATS, MinIO×3, Prometheus, Grafana) as lightweight manifests/StatefulSets in `deploy/k8s/infra/` — not hand-rolled HA, just enough to stand each dependency up in-cluster. This is a deliberate scope split already decided in docs/03-hld.md §5: infra gets minimal manifests, not fully-charted StatefulSets.
2. A Helm chart *we own* for `nimbus-api`, `nimbus-worker`, and the frontend — Deployments, Services, ConfigMap/Secret, an HPA stub, liveness/readiness probes wired to the existing `/healthz`/`/readyz` (and now `/metrics` — consider a ServiceMonitor or at least a scrape annotation if Prometheus is also going into the cluster).
3. `Dockerfile.web` + updating `deploy/docker-compose.yml` with a frontend service — the frontend has never been containerized (still `npm run dev` only). This was always bundled into Day 13, not deferred further.
4. Deploy the whole thing to a local `kind` cluster; `helm install`, confirm probes go green, confirm the app is reachable end-to-end (register → upload → download at minimum).
5. Verify against the real thing, not just `helm template` output — this project's whole pattern (see "Important context" below).

## Remaining tasks after Day 13 (roadmap order)

- Day 14: CI hardening — currently `.github/workflows/ci.yml` is lint+build only. Add unit test, integration test (`-tags=integration`, needs Postgres/Redis services in the CI job), and Docker build stages. Also wire in `scripts/chaos-node-kill.js` and `scripts/load-upload.js` from Day 12 (see docs/00-project-state.md's "Known issues/gaps" — this was explicitly deferred to Day 14, not forgotten). README gets an architecture diagram + demo script; the demo script should mention watching the `nimbus-storage-health` Grafana dashboard live during the chaos-kill demo.
- Day 15: buffer / final SRS Definition-of-Done pass (docs/01-srs.md §8).

## What Day 12 actually built (context for anything that touches chaos/testing)

- `scripts/chaos-node-kill.js` — full mid-upload chaos scenario. Built as `.js`, not the `.sh` docs/07 §5 originally sketched, because it needs real binary chunk PUTs plus precise timing control over exactly when `docker stop` fires — same reason Days 5-9's smoke scripts are Node, not bash. Run with `node scripts/chaos-node-kill.js [node-2]` against a live `docker compose up` stack. 10/10 assertions passed on the last verified run (down-detection in 5.1s, recovery in 2.1s).
- Targeted Go integration tests, gated behind `//go:build integration` (run with `go test -tags=integration ./...`, excluded from plain `go test ./...`):
  - `backend/internal/auth/refresh_integration_test.go` — proves refresh-token reuse revokes the *whole* rotation family, not just the reused token (docs/04-lld.md §3's compromise response). No smoke script exercises this since it needs holding onto an already-rotated token.
  - `backend/internal/upload/complete_race_integration_test.go` — proves the `CompleteUpload` compare-and-swap: 8 concurrent racers, exactly 1 wins, the rest get `ErrAlreadyCompleting`.
  - Both read `NIMBUS_TEST_POSTGRES_DSN`/`NIMBUS_TEST_REDIS_ADDR` env overrides — **don't assume `localhost:5432` reaches the compose Postgres on this dev machine**, see the new CLAUDE.md footgun (a native Windows PostgreSQL 17 install competes for the same host port). Run them from inside a container on the `nimbus_default` network if host-port results look like an auth failure that shouldn't be there.
- `scripts/load-upload.js` (k6) — ramps to 60 concurrent VUs, each driving the *real* chunked-upload flow (chunks/check → init → chunk-init → presigned PUT → commit → complete), not a coarser proxy endpoint. Must be run with `--network host` (see the new CLAUDE.md footgun about presigned MinIO URLs pointing at `localhost:900x`, which only resolves correctly on the same host Compose publishes those ports to). Last verified run: 3467 completed uploads over ~40s, 0% `http_req_failed`, 100% checks passed, all thresholds green.
- Two new, real, non-hypothetical footguns documented in CLAUDE.md from debugging this session — read them before assuming a "connection refused" or "password authentication failed" against this stack is a code bug.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: every feature gets built, then checked against the real running stack, not just code review or `helm template`/`kubectl apply --dry-run`. For Day 13 this means an actual `kind` cluster with the app actually reachable, not just manifests that look right.
- **Docs discipline**: when something diverges from the original plan, say so explicitly in the relevant doc rather than silently patching or leaving stale claims in place.
- **Known real gaps** (not yet built, don't assume otherwise): no rate limiting, no DLQ consumer, no `admin` module, frontend not containerized (Day 13's job), no k8s (Day 13's job), CI still doesn't run the Day 12 test suites (Day 14's job). Full list in docs/00-project-state.md.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled — if `docker compose up` fails oddly on infra it previously ran fine, check for zombie `com.docker.*` processes before assuming a code problem.
- **A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432** (new this session, see CLAUDE.md) — `localhost:5432` from the host is ambiguous. Doesn't affect the app itself (which only ever talks to Postgres via the `postgres` compose service name), but will bite anything run from the host that hardcodes `localhost:5432`.
- **Presigned MinIO URLs only resolve correctly from the host Compose publishes their ports to** (new this session, see CLAUDE.md) — matters for Day 13's k8s work too: inside a cluster, presigned URLs need to point at whatever's actually reachable from outside the cluster (a NodePort/Ingress/LoadBalancer), not `localhost` — worth deciding deliberately during the Helm chart's `PublicEndpoint` config rather than discovering it the hard way like this session did with k6.
- MinIO's Go SDK does a live `GetBucketLocation` call during presigning unless `Region` is pinned in `minio.Options` — already fixed in `backend/internal/storage/presign.go`, don't reintroduce this if touching presigning code.
- Go's `nil` slice marshals to JSON `null`, not `[]` — bit us once in `upload.Repository.FindMissingChunks`.
- No git remote is configured for this repo yet (local-only) — don't assume `git push` is safe/expected without checking.
- The Day 11/12 sessions left the full docker compose stack running for verification. Check `docker compose ps` before assuming a clean slate.
