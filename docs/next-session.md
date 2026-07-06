# Handoff — Next Session

Updated at the end of governance session part 2 (2026-07-06, same day as Tier 4 and governance part 1). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## The 15-item feature backlog is COMPLETE, plus two governance sessions on top

**Tier 1 (#1–#6), Tier 2 (#7–#9), Tier 3 (#10–#13), and Tier 4 (#14–#15) are all done and verified live** — see docs/00-project-state.md items 17–21. Tier 4 (2026-07-06, item 21) closed the last two: #14 password reset (token table, Mailpit email, forgot/reset pages) and #15 TOTP 2FA (stdlib RFC 6238, two-step login, Security page), plus a user-requested share-link clipboard auto-copy. After that, two same-day governance sessions (items 22–23, user asked how org roles work, then approved both scopes) added platform-admin gating on `/v1/admin/*`, an owner/admin-gated org usage view, a three-tier org RBAC ladder (owner > admin > member), and a UI naming split ("Cluster" for infra vs. "Members" for org governance).

## Next objective: the go-public VPS plan (blocked on the user)

The one standing open thread. Full agreed recipe is in docs/00-project-state.md "Known issues" first bullet — don't re-derive it (VPS + Compose + Caddy/Let's Encrypt TLS in front of api **and** each MinIO node, DuckDNS subdomain, fresh secrets, narrowed CORS, `NEXT_PUBLIC_API_URL` set in Vercel and redeploy). **Prereq from the user: a VPS IP + SSH key — ask, don't assume.** All backend blockers (rate limiting, caps/quotas, DLQ visibility — Tier 2) are done.

If the user wants more feature work instead, candidates that have come up but were never scoped/approved: Grafana alerting on new dead events, Helm-chart CI (`helm lint`/template/kind smoke), mailpit-on-kind, a change-role endpoint (today role changes are remove + re-add). Treat these as suggestions to offer, not a backlog to burn down. Note the Helm chart's ConfigMap doesn't set `NIMBUS_PLATFORM_ADMIN_EMAILS`, so on kind nobody is a platform admin until it's added or set manually — same Compose-only posture as mailpit.

## What the two governance sessions built (2026-07-06, after Tier 4)

See docs/00-project-state.md items 22–23. Notes worth carrying:

- **Two kinds of "admin," deliberately separate**: `/v1/admin/*` is cluster ops (nodes/ring/DLQ), gated by `users.is_platform_admin` (migration 000012, bootstrapped from `NIMBUS_PLATFORM_ADMIN_EMAILS` — compose promotes `tier1-demo@nimbus.dev`, `test@gmail.com`, `test1@gmail.com`). `GET /v1/orgs/{orgId}/usage` and member management are org governance, gated by org role (admin tier and up). Don't collapse them — the nav item is named "Cluster" specifically to keep them apart.
- **Org roles are a three-tier ladder** (migration 000013): owner > admin > member, rank-compared in `org.RequireRole`. Admin bounds live in `org.Service`, not the UI: admins can only *grant* `member` (`ErrElevatedRoleNeedsOwner` otherwise) and only *remove* plain members (`ErrAdminRemovesMembersOnly` otherwise) — a delegated admin can never consolidate control. The creator stays unremovable by anyone. No change-role endpoint exists yet (today: remove + re-add).
- **Promotion is promote-only** (`auth.Repository.PromotePlatformAdmins`): dropping an email from the env never revokes; revoke = manual `UPDATE users SET is_platform_admin = false`. A user registering *after* boot with a listed email isn't promoted until the next api restart.
- **`GET /v1/auth/me` exists now** — the frontend uses it to hide the Cluster nav for non-admins and to make the Members page role-aware (invite form and remove buttons only shown to roles that could actually use them). Server-side checks are the real enforcement; `/me` is presentation only.
- **Usage privacy line** (documented in `org/usage.go`): aggregate action metadata only, all already member-visible via the activity feed; no file names/content, and explicitly not `auth_audit_log` (spans orgs, user-private).
- **Full demo role ladder now exists** in Tier1 Demo Org: `tier1-demo@nimbus.dev` / `tier1-demo-password` (owner + platform admin), `org-admin-demo@nimbus.dev` / `org-admin-demo-pass` (org admin), `governance-demo@nimbus.dev` / `governance-demo-pass` (plain member). Log into each to see the Members/Cluster pages reshape by role.

## What the Tier 4 session built (2026-07-06, before the governance sessions)

See docs/00-project-state.md item 21 for the full list. Notes worth carrying:

- **TOTP is hand-rolled on the Go stdlib** (`internal/auth/totp.go`, ~70 lines) — deliberate, no OTP dependency; unit tests pin it to RFC 6238's Appendix B vectors. Don't swap in a library "for safety"; the vectors are the safety.
- **Replay guard**: every successful code verification burns its 30s time step in Redis (`nimbus:totp:used:<user>:<step>`, SetNX) — shared across login/confirm/disable. When testing flows back-to-back, use adjacent steps (±1 skew is accepted) or wait 30s; reusing the exact code you just used is *supposed* to fail.
- **Login challenge** is a 32-byte token hashed into Redis (`nimbus:totp:challenge:<sha256>`, 5-min TTL); a wrong code deliberately leaves it alive (retype), success consumes it. A pending (unconfirmed) enrollment does NOT gate login — only `confirmed_at IS NOT NULL` does.
- **Password reset revokes all refresh families in the same transaction** that consumes the token and rewrites the hash (`auth.Repository.ResetPassword`). Reset does not touch TOTP.
- **No SMTP configured → links are logged, not sent** (`auth.Service.ForgotPassword`); that's the kind/k8s behavior today since mailpit is Compose-only. Mailpit UI: http://localhost:8025, API `/api/v1/search?query=to:"<email>"`.
- **Clipboard auto-copy silently degrades**: `navigator.clipboard.writeText` throws when the document isn't focused (headless/automation) — the catch keeps the manual Copy button as fallback.
- Frontend gained `qrcode` (+`@types/qrcode`) for TOTP enrollment QR, and `jszip` for the public share pages' "Download folder"/"Download all" buttons (client-side zip, plans fetched just-in-time per file). Bundle zipping dedupes same-named files with a `(n)` suffix since a bundle has no folder structure to namespace them.
- The multi-select checkbox in the app's file browser now uses a custom `components/ui/Checkbox` — the raw native checkbox rendered as a stark white square unchecked (browser-default, unthemeable) against the dark UI. Reuse that component for any future checkbox rather than reintroducing the native look.

## Important context (carried forward, still true)

- **Verification pattern to keep using**: build, then check against the real thing — real API smokes (23 assertions for Tier 4, 21 for governance part 1, 15 for the role tier) plus full browser flows per role/perspective are the expected depth.
- **Known real gaps**: HPA stub inert, Helm chart not in CI (and doesn't set `NIMBUS_PLATFORM_ADMIN_EMAILS`), mailpit Compose-only, chaos/load/smoke scripts Compose-only. Full list in docs/00-project-state.md.
- **Environment state at session end**: full Compose stack up (`nimbus-web`, `mailpit` included) with images rebuilt from this session's code; migrations through 000013 applied. Demo users: see the role ladder above, plus `tier4-browser@nimbus.dev` / `tier4-browser-newpw` (**TOTP enabled** — logging in needs its authenticator secret, which only lives in that browser session's enrollment; if it's in the way, clear `user_totp` for that user in Postgres). The Day 13 `kind` cluster node container remains stopped, not deleted.
- A git remote is configured (`origin` → `github.com/Ritesh956/nimbus-storage-platform`, private). Commit and push at the end of each session, confirming with the user before each push. Branch protection is live on `main` (4 CI jobs required for PR merges; direct pushes still work).

## Warnings

- Docker Desktop on this machine has previously had a stale-socket issue after being killed/reinstalled; a one-off `TLS handshake timeout` pulling a new image was fixed with a direct `docker pull` retry.
- A native Windows PostgreSQL 17 install competes with Docker Desktop for host port 5432 — run integration tests inside the compose network (see CLAUDE.md footguns).
- `kind`/`helm`/`act`/`gh` (and `go` itself in fresh shells) aren't reliably on `PATH` — `go` lives at `C:\Program Files\Go\bin\go.exe`.
- Compose and kind can't run simultaneously (same host ports). Check what's actually up before assuming.
