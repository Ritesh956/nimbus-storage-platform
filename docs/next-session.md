# Handoff — Next Session

Updated at the end of the governance session (2026-07-06, same day as Tier 4). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## What the governance session built (2026-07-06, after Tier 4 — user asked how roles work, then approved this scope)

See docs/00-project-state.md item 22. Notes worth carrying:

- **Two kinds of "admin" now, deliberately separate**: `/v1/admin/*` is cluster ops (nodes/ring/DLQ), gated by `users.is_platform_admin` (migration 000012, bootstrapped from `NIMBUS_PLATFORM_ADMIN_EMAILS` — compose promotes `tier1-demo@nimbus.dev`, `test@gmail.com`, `test1@gmail.com`); `GET /v1/orgs/{orgId}/usage` is org governance, gated by org role (admin tier and up since part 2). Don't collapse them — the nav item is even named "Cluster" now to keep them apart.
- **Org roles are a three-tier ladder since part 2** (migration 000013): owner > admin > member, rank-compared in `org.RequireRole`. Admin bounds live in `org.Service` (grant member-only, remove plain-members-only) — don't enforce them in the UI alone; the Members page is role-aware purely to avoid offering 403s. No change-role endpoint exists (remove + re-add). Demo ladder: `tier1-demo@nimbus.dev` (owner+platform admin) / `org-admin-demo@nimbus.dev` `org-admin-demo-pass` (admin) / `governance-demo@nimbus.dev` (member).
- **Promotion is promote-only** (`auth.Repository.PromotePlatformAdmins`): dropping an email from the env never revokes; revoke = manual `UPDATE users SET is_platform_admin = false`. A user registering *after* boot with a listed email isn't promoted until the next api restart — acceptable for a bootstrap mechanism, worth knowing before "it didn't work" confusion.
- **`GET /v1/auth/me` exists now** — the frontend uses it to hide the Admin nav for non-admins (AppShell) and to render a readable access card on direct /admin navigation. The role checks are still server-side; /me is presentation only.
- **Usage privacy line** (documented in `org/usage.go`): aggregate action metadata only, all already member-visible via the activity feed; no file names/content, and explicitly not `auth_audit_log` (spans orgs, user-private).
- Demo accounts: `governance-demo@nimbus.dev` / `governance-demo-pass` is a plain member of Tier1 Demo Org (kept to demo the member-vs-owner contrast).

## The 15-item feature backlog is COMPLETE

**Tier 1 (#1–#6), Tier 2 (#7–#9), Tier 3 (#10–#13), and Tier 4 (#14–#15) are all done and verified live** — see docs/00-project-state.md items 17–21. Tier 4 (2026-07-06, item 21) closed the last two: #14 password reset (token table, Mailpit email, forgot/reset pages) and #15 TOTP 2FA (stdlib RFC 6238, two-step login, Security page), plus a user-requested share-link clipboard auto-copy.

## Next objective: the go-public VPS plan (blocked on the user)

The one standing open thread. Full agreed recipe is in docs/00-project-state.md "Known issues" first bullet — don't re-derive it (VPS + Compose + Caddy/Let's Encrypt TLS in front of api **and** each MinIO node, DuckDNS subdomain, fresh secrets, narrowed CORS, `NEXT_PUBLIC_API_URL` set in Vercel and redeploy). **Prereq from the user: a VPS IP + SSH key — ask, don't assume.** All backend blockers (rate limiting, caps/quotas, DLQ visibility — Tier 2) are done.

If the user wants more feature work instead, candidates that have come up but were never scoped/approved: ~~per-org usage endpoint~~ and ~~a delegated org-admin role tier~~ (both built in the governance sessions), Grafana alerting on new dead events, Helm-chart CI (`helm lint`/template/kind smoke), mailpit-on-kind, a change-role endpoint (today role changes are remove + re-add). Treat these as suggestions to offer, not a backlog to burn down. Note the Helm chart's ConfigMap doesn't set `NIMBUS_PLATFORM_ADMIN_EMAILS`, so on kind nobody is a platform admin until it's added or set manually — same Compose-only posture as mailpit.

## What the Tier 4 session actually built (2026-07-06)

See docs/00-project-state.md item 21 for the full list. Notes worth carrying:

- **TOTP is hand-rolled on the Go stdlib** (`internal/auth/totp.go`, ~70 lines) — deliberate, no OTP dependency; unit tests pin it to RFC 6238's Appendix B vectors. The Tier 4 smoke (scratchpad, not committed) re-implemented TOTP independently in Node and the two agreed. Don't swap in a library "for safety"; the vectors are the safety.
- **Replay guard**: every successful code verification burns its 30s time step in Redis (`nimbus:totp:used:<user>:<step>`, SetNX) — shared across login/confirm/disable. When testing flows back-to-back, use adjacent steps (±1 skew is accepted) or wait 30s; reusing the exact code you just used is *supposed* to fail.
- **Login challenge** is a 32-byte token hashed into Redis (`nimbus:totp:challenge:<sha256>`, 5-min TTL); a wrong code deliberately leaves it alive (retype), success consumes it. A pending (unconfirmed) enrollment does NOT gate login — only `confirmed_at IS NOT NULL` does.
- **Password reset revokes all refresh families in the same transaction** that consumes the token and rewrites the hash (`auth.Repository.ResetPassword`). Reset does not touch TOTP — verified explicitly.
- **No SMTP configured → links are logged, not sent** (`auth.Service.ForgotPassword`); that's the kind/k8s behavior today since mailpit is Compose-only (documented gap). Mailpit UI: http://localhost:8025, API `/api/v1/search?query=to:"<email>"`.
- **Clipboard auto-copy silently degrades**: `navigator.clipboard.writeText` throws when the document isn't focused (headless/automation) — the catch keeps the manual Copy button as fallback. The "Copied" label is only set after a successful write, so it doubles as the verification signal.
- Frontend gained two new npm dependencies: `qrcode` (+`@types/qrcode`) for the enrollment QR data-URL, and `jszip` for the public share pages' "Download folder"/"Download all" buttons (same-session follow-ups: client-side zip of a shared folder or bundle, plans fetched just-in-time per file — see docs/00-project-state.md item 21). Bundle zipping dedupes same-named files with a `(n)` suffix since a bundle has no folder structure to namespace them.
- The multi-select checkbox in the app's file browser (bundle-share picker) now uses a custom `components/ui/Checkbox` — the raw native checkbox rendered as a stark white square unchecked (browser-default, unthemeable) against the dark UI. If any other raw `<input type="checkbox">` shows up in future work, reuse that component rather than reintroducing the native look.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: build, then check against the real thing — this session's 23-assertion API smoke + full browser flows (UI enrollment, two-step login with computed codes, email-link reset) are the expected depth.
- **Known real gaps**: no per-org usage endpoint (FR-26, explicit v1 non-goal), HPA stub inert, Helm chart not in CI, mailpit Compose-only, chaos/load/smoke scripts Compose-only. Full list in docs/00-project-state.md.
- **Environment state at session end**: full Compose stack up (including `nimbus-web` and the new `mailpit`) with images rebuilt from this session's code; migrations through 000011 applied. Demo users: `tier1-demo@nimbus.dev` / `tier1-demo-password` (no TOTP), `tier4-browser@nimbus.dev` / `tier4-browser-newpw` (**TOTP enabled** — logging in needs its authenticator secret, which only lives in that browser session's enrollment; if it's in the way, clear `user_totp` for that user in Postgres). The Day 13 `kind` cluster node container remains stopped, not deleted.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each session, confirming with the user before each push (this session's push was pre-approved in the kickoff message). Branch protection is live on `main` (4 CI jobs required for PR merges; direct pushes still work).

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled; this session also saw a one-off `TLS handshake timeout` pulling a new image — a direct `docker pull` retry fixed it.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432 — run integration tests inside the compose network (see CLAUDE.md footguns).
- `kind`/`helm`/`act`/`gh` (and `go` itself in fresh shells) aren't reliably on `PATH` — `go` lives at `C:\Program Files\Go\bin\go.exe`.
- Compose and kind can't run simultaneously (same host ports). Check what's actually up before assuming.
