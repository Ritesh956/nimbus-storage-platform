# Handoff — Next Session

Updated at the end of the Tier 4 session (2026-07-06). Read [docs/00-project-state.md](00-project-state.md) first — it's the up-to-date source of truth; this file is about what to do next, not what's already true.

## The 15-item feature backlog is COMPLETE

**Tier 1 (#1–#6), Tier 2 (#7–#9), Tier 3 (#10–#13), and Tier 4 (#14–#15) are all done and verified live** — see docs/00-project-state.md items 17–21. Tier 4 (2026-07-06, item 21) closed the last two: #14 password reset (token table, Mailpit email, forgot/reset pages) and #15 TOTP 2FA (stdlib RFC 6238, two-step login, Security page), plus a user-requested share-link clipboard auto-copy.

## Next objective: the go-public VPS plan (blocked on the user)

The one standing open thread. Full agreed recipe is in docs/00-project-state.md "Known issues" first bullet — don't re-derive it (VPS + Compose + Caddy/Let's Encrypt TLS in front of api **and** each MinIO node, DuckDNS subdomain, fresh secrets, narrowed CORS, `NEXT_PUBLIC_API_URL` set in Vercel and redeploy). **Prereq from the user: a VPS IP + SSH key — ask, don't assume.** All backend blockers (rate limiting, caps/quotas, DLQ visibility — Tier 2) are done.

If the user wants more feature work instead, candidates that have come up but were never scoped/approved: per-org usage endpoint (FR-26, explicit v1 non-goal), Grafana alerting on new dead events, Helm-chart CI (`helm lint`/template/kind smoke), mailpit-on-kind. Treat these as suggestions to offer, not a backlog to burn down.

## What the Tier 4 session actually built (2026-07-06)

See docs/00-project-state.md item 21 for the full list. Notes worth carrying:

- **TOTP is hand-rolled on the Go stdlib** (`internal/auth/totp.go`, ~70 lines) — deliberate, no OTP dependency; unit tests pin it to RFC 6238's Appendix B vectors. The Tier 4 smoke (scratchpad, not committed) re-implemented TOTP independently in Node and the two agreed. Don't swap in a library "for safety"; the vectors are the safety.
- **Replay guard**: every successful code verification burns its 30s time step in Redis (`nimbus:totp:used:<user>:<step>`, SetNX) — shared across login/confirm/disable. When testing flows back-to-back, use adjacent steps (±1 skew is accepted) or wait 30s; reusing the exact code you just used is *supposed* to fail.
- **Login challenge** is a 32-byte token hashed into Redis (`nimbus:totp:challenge:<sha256>`, 5-min TTL); a wrong code deliberately leaves it alive (retype), success consumes it. A pending (unconfirmed) enrollment does NOT gate login — only `confirmed_at IS NOT NULL` does.
- **Password reset revokes all refresh families in the same transaction** that consumes the token and rewrites the hash (`auth.Repository.ResetPassword`). Reset does not touch TOTP — verified explicitly.
- **No SMTP configured → links are logged, not sent** (`auth.Service.ForgotPassword`); that's the kind/k8s behavior today since mailpit is Compose-only (documented gap). Mailpit UI: http://localhost:8025, API `/api/v1/search?query=to:"<email>"`.
- **Clipboard auto-copy silently degrades**: `navigator.clipboard.writeText` throws when the document isn't focused (headless/automation) — the catch keeps the manual Copy button as fallback. The "Copied" label is only set after a successful write, so it doubles as the verification signal.
- Frontend gained two new npm dependencies: `qrcode` (+`@types/qrcode`) for the enrollment QR data-URL, and `jszip` for the public share page's "Download folder" button (a same-session follow-up: recursive client-side zip of a shared folder, plans fetched just-in-time per file — see docs/00-project-state.md item 21).

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
