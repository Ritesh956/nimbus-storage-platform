# Contributing to Nimbus Storage Platform

This project has been solo-developed to date with a deliberate direct-to-`main`
workflow — no feature branches, no PR review, because there's been no one to
review against. That pattern is documented as an accepted trade-off, not an
oversight (see `docs/00-project-state.md`'s repo-hygiene notes). This file
exists so that trade-off has somewhere to end the moment a second contributor
shows up, rather than needing to be reconstructed from scratch.

## Before you start

Read [`docs/00-project-state.md`](docs/00-project-state.md) first — it's the
authoritative "what's actually true right now" snapshot (architecture,
completed features, known gaps, confirmed design decisions) and wins over any
other doc that disagrees with it. `CLAUDE.md` (repo root) has the day-to-day
working conventions (module boundaries, verification expectations, known
footguns on this stack) — they apply to human contributors exactly as much
as to an AI agent working in this repo.

## Branching and commits

- **Feature branches + PRs, not direct pushes to `main`.** `main` has GitHub
  branch-protection requiring the four CI jobs (`build-test`, `frontend`,
  `integration-test`, `docker-build`) to pass before merge — that gate exists
  either way, but a PR gives a second contributor's change a point-in-time
  review before it's live, which direct-to-`main` never had.
- Commit messages should separate *what* changed from *why* — the existing
  history (`git log`) is the style guide; read a handful of recent commits
  before writing your first one here.
- Don't `--amend` or force-push a commit another contributor might have based
  work on. Don't skip pre-commit/CI hooks (`--no-verify`) to get a red build
  green.

## Before opening a PR

- **Verify against a real running stack, not just code review** — this
  project's own stated philosophy (`CLAUDE.md`), and it has caught real bugs
  (CORS, search tokenization, a Windows path-with-spaces Docker mount bug)
  that review alone missed. Backend: the `scripts/smoke-*.sh`/`.js` scripts
  and `go test -tags=integration ./...` against real Postgres/Redis/MinIO/
  NATS. Frontend: `npm test` (Vitest) plus a live browser pass for anything
  UI-visible.
- `go build ./... && go vet ./... && gofmt -l .` clean; `npm run lint && npm
  run build` clean; `go test ./... -short` green.
- If you touched CI itself, validate the workflow locally with `act -P
  ubuntu-latest=catthehacker/ubuntu:act-latest` before trusting it (see
  `CLAUDE.md`'s footgun notes for the exact flags this machine needs).

## Scope discipline

This project follows the engineering norms in `CLAUDE.md` strictly: no
abstractions, error handling, or config beyond what the change actually
needs. A bug fix doesn't need surrounding cleanup; a one-shot script doesn't
need a config file. When in doubt, match the smallest existing pattern
already in the module you're touching rather than inventing a new one.

## Cross-module boundaries

No module reaches into another's Postgres tables directly (see
`docs/08-folder-structure.md`). Cross-module reads/writes go through small
interfaces (e.g. `FileCreator`, `MembershipChecker`) satisfied by adapters in
`backend/cmd/api/main.go`'s `wire_*.go` files.

## Backups

`scripts/backup.sh [compose|kind]` / `scripts/restore.sh <dir> [compose|kind]`
snapshot and restore Postgres + all three MinIO nodes. Take a backup before
running a migration or any schema change against data you care about keeping.
