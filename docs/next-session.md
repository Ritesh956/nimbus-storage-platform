# Handoff — Next Session

Written at the end of the session that completed Days 1-10 (backend + frontend) and did a full documentation audit. Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective

**Day 11: Prometheus instrumentation + Grafana dashboards** (per [docs/09-roadmap.md](09-roadmap.md)).

Concretely:
1. Add a Prometheus client library to `nimbus-api` and `nimbus-worker`, expose `/metrics`.
2. Instrument: HTTP request histogram (route, status, latency), upload throughput, chunk placement failures, per-node health gauge, NATS consumer lag.
3. Add `prometheus` + `grafana` services to `deploy/docker-compose.yml` (they're in the target architecture diagram in docs/02-system-design.md but not in compose yet — this is new work, not wiring up something half-done).
4. Build two Grafana dashboards: golden signals (api/worker), storage-node health/replication status (this second one is what should be on screen during the eventual chaos demo).
5. Verify under real traffic — run a few uploads/downloads through the running stack and confirm the dashboards populate, don't just confirm the endpoint returns data.

## Remaining tasks after Day 11 (roadmap order)

- Day 12: full mid-upload chaos test (`scripts/chaos-node-kill.sh` — `scripts/smoke-storage.sh` from Day 4 already covers node-down-at-rest, this is the harder "kill it while a write is in flight" case), integration test suite, k6 load test (NFR-2: ≥50 concurrent uploads).
- Day 13: Kubernetes manifests (`deploy/k8s/infra/` for stateful deps) + a Helm chart we own for `nimbus-api`/`nimbus-worker`/frontend, deploy to kind. Bundle in containerizing the frontend (`Dockerfile.web` + compose service) here too — it doesn't exist yet.
- Day 14: CI hardening (currently lint+build only — add unit/integration/docker-build stages), README with architecture diagram + demo script.
- Day 15: buffer / final SRS Definition-of-Done pass.

## Important context

- **Verification pattern to keep using**: every feature gets built, then checked against the real running stack (curl/smoke scripts for backend, live Claude Preview MCP browser sessions for frontend) — not just code review. This caught real bugs this session (search tokenization, missing CORS, a nil-slice JSON quirk, a render-time navigation crash, layout clipping). Don't skip this step for Day 11 — actually generate traffic and look at the dashboards.
- **Docs discipline**: when something diverges from the original plan, say so explicitly in the relevant doc rather than silently matching docs to code after the fact or leaving stale claims in place. This session's audit found and fixed several: 2-state vs 3-state breaker, Redis-read vs in-memory health, synchronous vs async activity writes, unimplemented rate limiting/admin-usage-endpoint/DLQ-remediation. Follow the same pattern for whatever Day 11 reveals.
- **Known real gaps** (not yet built, don't assume otherwise): no rate limiting, no DLQ consumer, no `admin` module, frontend not containerized, no k8s, CI is skeleton-only. Full list in docs/00-project-state.md.

## Files likely to change (Day 11)

- `backend/go.mod` / `go.sum` — new Prometheus client dependency.
- `backend/internal/platform/httpserver/` — likely a new `metrics.go` middleware for the HTTP histogram, plus wiring `/metrics` into the mux in `backend/cmd/api/main.go`.
- `backend/internal/storage/` — health gauge + placement-failure counter, most naturally added where `Resolve`/`probe` already live (`router.go`, `breaker.go`).
- `backend/cmd/worker/main.go` / `backend/internal/events/` — NATS consumer lag metric, worker's own `/metrics` endpoint.
- `deploy/docker-compose.yml` — add `prometheus` + `grafana` services.
- `deploy/observability/prometheus.yml` (new) — scrape config for api/worker.
- `deploy/observability/grafana/dashboards/` (new) — `golden-signals.json`, `storage-health.json`.
- `docs/03-hld.md` §2 (observability is currently listed as designed-but-not-built) and `docs/02-system-design.md` §7 — update once built, same "flag drift explicitly" pattern as this session.
- `docs/00-project-state.md` and `docs/09-roadmap.md` — update the Day 11 row/gap list once done.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled (see git history / session notes) — if `docker compose up` fails oddly on infra it previously ran fine, check for zombie `com.docker.*` processes before assuming a code problem.
- MinIO's Go SDK does a live `GetBucketLocation` call during presigning unless `Region` is pinned in `minio.Options` — already fixed in `backend/internal/storage/presign.go`, don't reintroduce this if touching presigning code.
- Go's `nil` slice marshals to JSON `null`, not `[]` — bit us once in `upload.Repository.FindMissingChunks`. Watch for this pattern anywhere a frontend does `.length` on an API array response.
- No git remote is configured for this repo yet (local-only, single commit history so far as of this session) — don't assume `git push` is safe/expected without checking.
