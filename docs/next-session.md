# Handoff — Next Session

Written at the end of the session that completed Day 15 (SRS Definition-of-Done pass — the last day on the original roadmap). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective

There is no more scheduled roadmap. All 15 days (Days 1-14 core build + Day 15 DoD pass) are done. The next session's job is to ask the user what they want next — likely one of docs/01-srs.md §6's documented post-v1 roadmap ideas (desktop/CLI sync, real RBAC, gRPC API, OTel tracing + Loki, Terraform + real cloud deploy, encryption-at-rest, automated rebalancing/compaction/GC, quotas/billing) — rather than assuming any particular one.

## What Day 15 actually built

- **FR-4 (auth audit log)**: new `auth_audit_log` table (migration `000005`), owned by `internal/auth` (not `internal/activity` — auth events have no org/target context to hang a row off of, unlike the org-scoped `activity_events` table). `auth.Service.Login/Refresh/Logout` each record a best-effort row (login/refresh/logout verbs), same non-fatal pattern as `upload.Service`'s activity recording. No API surface — it's a "basic" audit log, queried directly in Postgres. Verified end-to-end against the live `kind` cluster: register→login→refresh→logout produced exactly 3 rows.
- **FR-8 (read-side checksum verification)**: `processing.Processor.reassemble` (worker's thumbnail-generation path) now re-hashes each fetched chunk against its own content-addressed hash before accepting it (chunks are named by their SHA-256, so the object key already *is* the expected digest), falling back to the next replica on a mismatch, matching the existing fallback-on-I/O-error pattern. Verified against **real corrupted MinIO replicas** (manually corrupted a chunk's on-disk part file via `kubectl exec` into the MinIO pods): MinIO's own bitrot detection actually surfaces corruption as a read error (not silently-wrong bytes) before our hash check ever needs to fire, our code correctly logs and falls back to the next node, and when both replicas were deliberately corrupted, `Process()` correctly errored out (triggering NATS redelivery) rather than silently succeeding. The direct-to-client download path (`file.Service.DownloadPlan`, presigned MinIO URLs) still never touches the server and stays client-verified only — documented as an architectural fact in docs/01-srs.md FR-8, not a gap.
- **FR-26 narrowed**: per-org usage endpoint stays undone in v1 (user's explicit choice on Day 15, not a silent gap) — docs/01-srs.md FR-26 now says so directly.
- **NFR-4 actually measured for the first time**: new `scripts/load-metadata.js` (k6), same 60-VU concurrency as the NFR-2 upload load test but hitting the cheap `GET /v1/orgs/{orgId}/folders` metadata endpoint in isolation. Result: p95 20.5ms, comfortably under the 200ms bound (149k requests, 0% failures).
- **NFR-6 actually enforced**: GitHub branch protection added on `main` (via `gh api`) requiring `build-test`, `frontend`, `integration-test`, `docker-build` to pass before a PR can merge. Direct pushes to `main` still work (no PR requirement was added — that would've been a bigger workflow change than asked for); force-push and branch deletion are blocked.
- **docs/06-api-design.md checked route-by-route against `backend/cmd/api/main.go`** — found accurate, no drift. Bumped its status line, added a one-line note about the new (API-invisible) audit log.
- **README's demo-script cross-environment claim corrected**: "Steps 2-6 work identically against Compose or kind" was false for step 3 — `scripts/chaos-node-kill.js` hardcodes `docker compose stop`/`start` on a named container, which has no equivalent once MinIO runs as `kind` pods. Narrowed the claim to steps 2/4/5/6 and explained why, with the `kubectl delete pod` equivalent noted as an alternative demonstration under `kind`.
- **README's "<10 minutes" claim wasn't stopwatch-verified** — only a `kind` cluster was running (6+ hrs uptime, real test data), and the user chose a careful re-read over tearing it down for a timed Compose run. Worth an actual timed run next time Compose is started genuinely fresh (no Docker build cache) — see docs/00-project-state.md's Known issues.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: build, then check against the real thing — this session's FR-8 verification (deliberately corrupting a live MinIO replica's on-disk bytes via `kubectl exec`, not just reading the code) is a good example of the depth this project's convention expects.
- **Known real gaps** (don't assume otherwise): no rate limiting, no DLQ consumer, no `internal/admin` module, no per-org usage endpoint (FR-26, now explicitly a v1 non-goal), HPA stub inert (no metrics-server), Helm chart not in CI, `chaos-node-kill.js`/`smoke-folders.sh`/`smoke-thumbnails.js` are Compose-only (hardcoded `docker compose`/`docker exec <container-name>`, no kind equivalent). Full list in docs/00-project-state.md.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each day/session, confirming with the user before each push. **Branch protection is now live on `main`** — direct pushes still work, but PRs need the 4 CI jobs green to merge.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432.
- `kind`/`helm`/`act`/`gh` were all installed via `winget` but aren't reliably on `PATH` in fresh shells on this machine — check `winget list` / `$env:LOCALAPPDATA\Microsoft\WinGet\Packages\...` (or `/c/Program Files/GitHub CLI/gh.exe` for `gh` specifically, which winget didn't put under the usual WinGet Packages dir) before assuming a tool isn't installed.
- The `kind` cluster from Day 13 was still running at the start of this Day 15 session (6+ hrs uptime) — Compose and kind can't run simultaneously (same host ports). Check `kubectl get nodes` before assuming either is (or isn't) up.
