# Handoff — Next Session

Updated at the end of the Tier 3 backlog session (2026-07-05). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## Next objective: finish the agreed feature backlog (Tier 4), then the go-public VPS plan

All 15 roadmap days are done, plus the Dashdark X/Vercel session, plus the Tier 1, Tier 2, and Tier 3 sessions (all 2026-07-05). **Tiers 1–3 are complete and verified live** — see docs/00-project-state.md items 17–19. Next per the agreed sequence: **Tier 4** (#14 password reset, then #15 TOTP 2FA). The go-public VPS plan stays unblocked backend-side; its remaining prereq is the user provisioning a VPS (their account/payment — ask, don't assume).

Full backlog as presented and accepted:

**Tier 1 — backend already does it, UI doesn't show it — ✅ ALL DONE** (#1 thumbnails, #2 members UI, #3 move UI, #4 search filters/pagination, #5 share expiry, #6 breadcrumbs)

**Tier 2 — blockers before the VPS go-public plan — ✅ ALL DONE** (#7 rate limiting, #8 upload caps + org quotas, #9 DLQ visibility)

**Tier 3 — distributed-systems flex — ✅ ALL DONE (2026-07-05 session)**
10. ~~**Chunk garbage collection**~~ — done: `internal/gc` mark/sweep in the worker, dedup-lease + resurrection protocol, migrations 000007/000008, `scripts/smoke-gc.js` (13 assertions). Design + residual race documented in docs/07 §6.
11. ~~**Trash auto-purge**~~ — done: FR-11 retention window enforced from the same GC tick (`gc.TrashPurger` port; folder purge guarded by a subtree liveness check).
12. ~~**Live updates via SSE**~~ — done: `GET /v1/orgs/{orgId}/events` (`internal/live`, Redis pub/sub relay), `useLiveEvents` frontend hook; node kills flip red live, thumbnails pop in without refresh.
13. ~~**Hash-ring visualization**~~ — done: `GET /v1/admin/ring[?file_id=]` + admin-page SVG ring (vnodes, per-node colors, live dimming, file chunk plotting with preference-vs-recorded-locations).

**Tier 4 — auth polish (the remaining items)**
14. **Password reset flow** — token table + email via a local Mailpit container (free SMTP catcher).
15. **TOTP 2FA** — standard library, pairs with the existing auth_audit_log.

After (or alongside) Tier 4, the other open thread is the **deferred go-public plan**: full agreed recipe in docs/00-project-state.md "Known issues" — don't re-derive it. Prereq from the user: a VPS IP + SSH key.

## What the Tier 3 session actually built (2026-07-05, after Tier 2)

See docs/00-project-state.md item 19 for the full list. Notes worth carrying:

- **GC design decisions are deliberate** (docs/07 §6): refcount is *computed* (`NOT EXISTS` against `file_version_chunks`, new index), not a stored counter; the dedup check is a lease (reporting a chunk present touches `last_seen_at`; doomed chunks read as *missing* so clients re-upload and the commit resurrects them); sweep deletes objects **before** the row inside a `FOR UPDATE` transaction so a down node aborts cleanly and retries next tick. There is a documented, deliberately-unclosed residual race (client PUT racing the sweep's deletion transaction) — closing it needs generation-versioned object keys, explicitly out of v1 scope. Don't "fix" the computed refcount into a stored counter or reorder the sweep without re-reading docs/07 §6.
- **Migration 000008 fixed a real latent bug the GC suite caught**: `uploads.file_id/version_id/target_file_id` FKs silently broke `DELETE /v1/files/{id}/purge` for *every uploaded file* since Day 6 (FK violation → 500). If something else purge-adjacent misbehaves, suspect bookkeeping FKs first.
- **`internal/gc` deliberately queries other modules' tables** (`file_version_chunks`, `upload_chunks`) — a documented exception to the no-cross-module-SQL rule; the mark/sweep decisions must each be one atomic statement. The package doc comment explains why; don't decompose it into ports.
- **SSE architecture**: `activity.Service` is the funnel both processes' events pass through, so its new `LiveNotifier` port (satisfied by `live.Publisher` → Redis pub/sub) covers worker-side events with no NATS involvement. The storage router's health channel (`nimbus:health:changes`, now an exported const) already existed since Day 4 — the SSE endpoint just relays it. Frontend consumes via `fetch` + stream reader (EventSource can't send Authorization; a query-string token would leak into request logs). `httpserver.statusWriter` grew an `Unwrap()` so `http.ResponseController` can clear the server's write deadline per-connection — without it the 60s `WriteTimeout` kills streams.
- **Verification setup worth reusing**: `scripts/smoke-gc.js` needs short GC windows — a throwaway compose override (scratchpad, not committed) setting `NIMBUS_GC_INTERVAL=10s` / `NIMBUS_GC_GRACE=20s` on nimbus-worker. Re-create it when re-running the suite; production defaults are 10m/1h.
- **Environment state at session end**: full Compose stack up (including `nimbus-web`) with images rebuilt from this session's code, worker back on default GC windows (10m/1h — the short-window override was removed). Tier 1 seed data still present (user `tier1-demo@nimbus.dev` / `tier1-demo-password`, org "Tier1 Demo Org"); Pictures now also holds a `live-demo-*.png` from the SSE verification. The Day 13 `kind` cluster node container remains stopped, not deleted.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: build, then check against the real thing — this session's GC suite (real uploads, real purges, real MinIO object checks, a real node kill mid-sweep) and the SSE browser test (API-side upload appearing live in a watching browser) are the expected depth.
- **Known real gaps**: no per-org usage endpoint (FR-26, explicit v1 non-goal), HPA stub inert, Helm chart not in CI, chaos/load/smoke scripts are Compose-only (including the new `smoke-gc.js` — it shells out to `docker exec`/`docker compose`). Full list in docs/00-project-state.md.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each session, confirming with the user before each push. Branch protection is live on `main` (4 CI jobs required for PR merges; direct pushes still work).

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432 — run integration tests inside the compose network (see CLAUDE.md footguns).
- `kind`/`helm`/`act`/`gh` (and now also `go` itself in fresh shells) aren't reliably on `PATH` — `go` lives at `C:\Program Files\Go\bin\go.exe`.
- Compose and kind can't run simultaneously (same host ports). Check what's actually up before assuming.
