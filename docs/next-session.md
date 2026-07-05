# Handoff — Next Session

Updated at the end of the Tier 1 backlog session (2026-07-05). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective: continue the agreed feature backlog

All 15 roadmap days are done, plus the Dashdark X/Vercel session, plus the Tier 1 and Tier 2 sessions (both 2026-07-05). **Tiers 1 and 2 are complete and verified live** — see docs/00-project-state.md items 17–18. Next per the agreed sequence: **#10 chunk garbage collection** (the "best interview story" item), then the rest of Tier 3. The go-public VPS plan is also now unblocked backend-side (Tier 2 was its blocker set); it still needs the user to provision a VPS. Full backlog as presented and accepted:

**Tier 1 — backend already does it, UI doesn't show it — ✅ ALL DONE (2026-07-05 session)**
1. ~~**Show thumbnails in the UI**~~ — done: `GET /v1/files/{fileId}/thumbnail` (presigned targets, ring-preference healthy-first), `has_thumbnail`/size/mime on the children listing, FileRow thumb + preview modal.
2. ~~**Org members UI**~~ — done: `/app/org/{orgId}/members` page, invite-by-email + role pills + remove, 6-item nav.
3. ~~**Move files/folders UI**~~ — done: shared `MoveDialog` drill-down picker on file rows and folder rows.
4. ~~**Search filters + pagination UI**~~ — done: type/owner/date/size panel, removable chips, cursor "Load more".
5. ~~**Share expiry in the UI**~~ — done: 1h/1d/7d/never dropdown; expiry proven end-to-end (forced past-expiry in DB → public page refuses).
6. ~~**Breadcrumbs**~~ — done: `GET /v1/folders/{folderId}/path` + trail in the folder view.

**Tier 2 — blockers before the VPS go-public plan — ✅ ALL DONE (2026-07-05 session, same day as Tier 1)**
7. ~~**Rate limiting**~~ — done: `internal/platform/ratelimit` Redis token bucket (Lua, atomic), per-user via signature-only `PeekUserID` / per-IP fallback, 25 rps/burst 50 defaults, fail-open, health/metrics exempt.
8. ~~**Upload caps + org quotas**~~ — done: `NIMBUS_MAX_UPLOAD_BYTES` (100 MiB → 413 `too_large`) + `NIMBUS_ORG_QUOTA_BYTES` (10 GiB → 507 `quota_exceeded`), enforced at init and re-checked at complete.
9. ~~**DLQ visibility**~~ — done: `dead_events` table (migration 000006) written by the worker on final failed delivery, `GET /v1/admin/dlq` + retry endpoint, admin-page panel. Design revised from a DLQ subject to a Postgres table — see docs/07 §3.

**Tier 3 — distributed-systems flex (portfolio meat)**
10. **Chunk garbage collection** — content-addressed dedup means deleting a file never frees storage; purged files leave orphaned chunks in MinIO forever. Refcount chunks + background sweep in the worker (watch the in-flight-upload-references-chunk-being-reaped race). The most staff-level item here.
11. **Trash auto-purge** — FR-11 promises retention-window purge; no timer exists. Worker ticker, feeds into #10.
12. **Live updates via SSE** — activity/node-health currently poll; SSE makes thumbnails pop in and chaos-demo node failures flip red live.
13. **Hash-ring visualization** — admin-page ring diagram (vnodes + where a file's chunks landed); one debug endpoint + frontend.

**Tier 4 — auth polish**
14. **Password reset flow** — token table + email via a local Mailpit container (free SMTP catcher).
15. **TOTP 2FA** — standard library, pairs with the existing auth_audit_log.

After (or alongside) the backlog, the other open thread is the **deferred go-public plan**: the user wanted the backend on a VPS but held off since the VPS account/payment is theirs to create. Full agreed recipe is in docs/00-project-state.md "Known issues" — don't re-derive it. Prereq from the user: a VPS IP + SSH key. Tier 2 above is the blocker set for it.

## What the Tier 2 session actually built (2026-07-05, after Tier 1)

See docs/00-project-state.md item 18 for the full list. Notes worth carrying:

- **Quota semantics are deliberate:** usage is logical bytes (every stored version, trashed included) — dedup doesn't discount it, purge is what frees it. Both limits re-checked at `/complete` because init's declared size is client-supplied. New error codes `too_large` (413) / `quota_exceeded` (507) joined the envelope table.
- **Rate limiter fails open** on Redis errors and exempts `/healthz`/`/readyz`/`/metrics`. Keying uses `auth.Service.PeekUserID` (signature check only — deliberately no blacklist round-trip; it's an identity for bucketing, not an authorization decision).
- **DLQ is a Postgres table, not a NATS subject** — design revision recorded in docs/07 §3. The worker detects the final delivery inline (`msg.Metadata().NumDelivered >= maxDeliver`), inserts into `dead_events`, then Terms. Retry = republish stored payload; a republish that fails again simply dead-letters as a fresh row. The retry endpoint flips dead→retried *before* publishing (no double-publish on concurrent clicks) and reverts on publish failure.
- **Verification trick worth reusing:** to force a real DLQ event, commit a chunk, `rm -rf` its object dir from all 3 MinIO nodes (`docker exec nimbus-minio-node-N-1 rm -rf /data/nimbus-chunks/<hash>`), *then* call `/complete` — the worker fails all 5 deliveries (~111s with the backoff schedule). Restore by re-PUTting the bytes via a fresh upload session's presigned targets (no commit needed), then Retry from the admin UI.
- Tiny-limit overrides for testing 507/429 live in a throwaway compose override (scratchpad, not committed): `NIMBUS_ORG_QUOTA_BYTES=1000000`, `NIMBUS_RATE_LIMIT_RPS=2`, `NIMBUS_RATE_LIMIT_BURST=5` on nimbus-api.

## What the Tier 1 session actually built (2026-07-05)

See docs/00-project-state.md item 17 for the full feature list and docs/06-api-design.md for the three API surface changes (`/thumbnail`, `/path`, children-listing metadata). Notes worth carrying:

- **Thumbnail location is deliberately not recorded in Postgres.** The worker places a thumbnail on the first *healthy* node of the ring's deterministic preference list for `thumb:<versionID>` (`processing.Processor.Process`); the read path (`file.Service.ThumbnailTargets` via new `storage.Router.Candidates`) presigns the *whole* unfiltered preference list healthy-first and lets the client walk it, exactly like download-plan targets. Don't "fix" this by recording a location row unless thumbnails grow real replication.
- **`file.Repository.ListInFolder` now returns `ListEntry`** (File + latest-version size/mime/has-thumbnail via LEFT JOIN) — `folder.FileSummary` grew the same fields. Anything else that wants per-file display metadata in a listing should reuse that, not add round-trips.
- **Environment state at session end:** Compose stack up (images rebuilt with the new code), `nimbus-web` container *stopped* in favor of `npm run dev` on 3000 during verification (`docker compose -f deploy/docker-compose.yml start nimbus-web` to restore); the Day 13 `kind` cluster's node container was **stopped, not deleted** (`docker start nimbus-control-plane` brings it back — but not while Compose is up, same ports). Seed data for re-verification: user `tier1-demo@nimbus.dev` / `tier1-demo-password`, org "Tier1 Demo Org" (Home → Pictures/Documents, gradient.png has a real thumbnail, 25 report-NN.txt files in Documents for pagination).

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
- **Known real gaps** (don't assume otherwise): no `internal/admin` module (the admin reads live in `storage.Handler` and `events.DLQHandler`), no per-org usage endpoint (FR-26, explicitly a v1 non-goal — though `file.Repository.OrgUsageBytes` now exists internally for quota enforcement, so exposing it would be cheap if ever wanted), HPA stub inert (no metrics-server), Helm chart not in CI, `chaos-node-kill.js`/`smoke-folders.sh`/`smoke-thumbnails.js` are Compose-only (hardcoded `docker compose`/`docker exec <container-name>`, no kind equivalent). ~~No rate limiting / no DLQ consumer~~ — both built in the Tier 2 session. Full list in docs/00-project-state.md.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each day/session, confirming with the user before each push. **Branch protection is now live on `main`** — direct pushes still work, but PRs need the 4 CI jobs green to merge.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432.
- `kind`/`helm`/`act`/`gh` were all installed via `winget` but aren't reliably on `PATH` in fresh shells on this machine — check `winget list` / `$env:LOCALAPPDATA\Microsoft\WinGet\Packages\...` (or `/c/Program Files/GitHub CLI/gh.exe` for `gh` specifically, which winget didn't put under the usual WinGet Packages dir) before assuming a tool isn't installed.
- The `kind` cluster from Day 13 was still running at the start of this Day 15 session (6+ hrs uptime) — Compose and kind can't run simultaneously (same host ports). Check `kubectl get nodes` before assuming either is (or isn't) up.
