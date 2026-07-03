# Handoff — Next Session

Written at the end of the session that completed Day 14 (CI hardening, README architecture diagram + demo script). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective

**Day 15: buffer / final SRS Definition-of-Done pass** (per [docs/09-roadmap.md](09-roadmap.md) and [docs/01-srs.md](01-srs.md) §8).

The DoD text: *"Every FR in §3 implemented and demoed; the chaos scenario (FR-21) runs reproducibly via a documented script; CI green; README walkthrough lets a stranger clone, `docker compose up`, and exercise the full flow ... in under 10 minutes."*

This is a **verification and cleanup pass**, not a feature-building day — go through docs/01-srs.md §3 FR-by-FR and §5 NFR-by-NFR and confirm each is actually true, not just plausible. Two real gaps were already found during a Day 14 cross-check (see docs/00-project-state.md "Known issues" for full context) — start there rather than re-discovering them:

1. **FR-4 (auth audit log: login/refresh/logout) doesn't exist.** `internal/activity` only records file/org events. Decide with the user: build it (small — a few `activity.Record*` calls from `auth.Service`), or explicitly demote it to a documented non-goal. Either is fine; silently ignoring it is not.
2. **FR-8's "checksum verification ... on read" is client-facing only.** The checksum is returned in API responses for a client to check itself; nothing server-side re-verifies reassembled bytes on the read path. Same choice: build minimal server-side verification (e.g., in `processing.Processor.reassemble`, which already has the full byte stream in memory), or document the narrower scope explicitly in docs/01-srs.md and docs/00-project-state.md.

Beyond those two, a suggested pass order (don't just re-verify everything blindly — spot-check the ones most likely to have drifted):

- **FR-26 admin view**: "storage node health, per-org usage" — node health exists (`/v1/admin/nodes` + the frontend admin page); per-org usage does not (`GET /v1/admin/orgs/{orgId}/usage` was never built, already a documented gap). FR-26 is half-done; decide whether that's acceptable for v1 or needs the usage endpoint.
- **NFR-4 (p95 API latency <200ms for metadata operations)**: never explicitly measured. `scripts/load-upload.js`'s p95 (~735ms) is for the *full chunked-upload flow* (5 API calls + 2 MinIO PUTs per iteration), not pure metadata-operation latency — it doesn't actually verify this NFR. Consider adding a small k6 (or even `hey`/`ab`) run against a cheap metadata endpoint (e.g. `GET /v1/orgs/{orgId}/folders`) under load, or explicitly note in docs/01-srs.md that this NFR wasn't isolated and measured separately.
- **NFR-6 (CI must pass lint+unit+integration before merge)**: satisfied as of Day 14, but double-check there's no branch-protection-rule gap (GitHub branch protection isn't something this session's tooling can configure — that's a repo-settings action for the user to do, see docs/00's Known issues if it needs flagging there).
- **The README's own 10-minute claim**: actually time a fresh-clone walkthrough (or at least a re-read pretending you're a stranger) rather than assuming the Day 14 write-up is accurate.
- **`docs/06-api-design.md`**: hasn't been touched since early sessions — check it still matches the real routes in `backend/cmd/api/main.go` (Days 11-14 didn't add routes, but worth a pass since this doc's staleness wasn't checked recently).

## What Day 14 actually built (context if CI needs touching again)

- `.github/workflows/ci.yml` now has 4 jobs: `build-test` (unchanged: vet/build/unit), `frontend` (new: `npm install && npm run lint && npm run build` — `npm install` not `npm ci`, see CLAUDE.md's footgun note), `integration-test` (new: real `postgres`/`redis` service containers, migrations applied via `docker run --network host migrate/migrate`, then `go test -tags=integration ./...`), `docker-build` (extended: now builds all four images, api/worker/web/migrate, not just api/worker).
- Chaos (`scripts/chaos-node-kill.js`) and load (`scripts/load-upload.js`) tests were explicitly decided (with the user) to stay **out** of CI — real multi-container infra + real wall-clock time is a different cost/value trade-off than a per-push gate. Don't add them without a similar explicit check-in; re-raising the same question is fine, silently changing the decision isn't.
- All new CI logic was validated locally before trusting it: `act -j frontend` and `act -j build-test` (both passed end-to-end via Docker-based GitHub Actions emulation), and the `integration-test` job's exact command sequence against throwaway Postgres/Redis containers on non-conflicting ports (the native-Postgres-on-5432 footgun applies to ad-hoc local testing too, not just the `-tags=integration` test helpers).
- README rewrite: added the Mermaid architecture diagram (reused verbatim from docs/02-system-design.md §1 — GitHub renders Mermaid natively), a Week 3 status breakdown (Days 11-14), both the Compose and kind "Running locally" paths with the port-sharing caveat, and a 6-step numbered demo script matching the SRS DoD's flow (register → upload → kill a node mid-upload → share → thumbnail/activity → optional load test).
- `act`, installed this session via `winget install nektos.act`, is available for validating future CI changes locally before pushing — see its usage above.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: build, then check against the real thing. For CI specifically, that now means `act` locally before assuming a workflow YAML change is correct, not just "it looks right."
- **Docs discipline**: Day 14 found stale/missing gaps in docs/00-project-state.md that had existed for a couple of days without being caught (FR-4, FR-8) — a reminder that "known issues" lists need periodic re-derivation from the actual SRS, not just incremental updates. Day 15 is exactly that kind of pass.
- **Known real gaps** (don't assume otherwise): no rate limiting, no DLQ consumer, no `admin` module, no per-org usage endpoint, HPA stub inert (no metrics-server), Helm chart not in CI, no auth audit log (FR-4), no server-side read-path checksum verification (FR-8). Full list in docs/00-project-state.md.

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432 — doesn't affect the app itself, but bites anything run from the host (including ad-hoc CI-logic validation) that hardcodes `localhost:5432`.
- `kind`/`helm`/`act` were all installed via `winget` this project's sessions but aren't reliably on `PATH` in fresh shells on this machine — check `winget list` / `$env:LOCALAPPDATA\Microsoft\WinGet\Packages\...` before assuming a tool isn't installed.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each day/session, confirming with the user before each push.
- The kind cluster from Day 13 may still be running (check `kubectl get nodes`) — Compose and kind can't run simultaneously (same host ports).
