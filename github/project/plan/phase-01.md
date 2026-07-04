# Phase 1 — The stateless connector module skeleton

*Realizes design Decision 1 (stateless connector on the appkit chassis).
Structural — owns no `R-` ids. Depends on no earlier phase.*

## What gets built

The `github` module skeleton, entirely within `github/`:

- `github/go.mod` — `module github`, `go 1.26`, with the same `replace appkit =>
  ../appkit` (and `replace eventplane => ../eventplane`) directives `gmail` uses,
  and the chassis dependency set. `github/VERSION` seeded `v0.1.0`. `Makefile`,
  `.gitignore`, `.envrc` (`source_up` plus the three `cat ~/.secrets/IKIGENBA_APP_*`
  / `IKIGENBA_GITHUB_ORG` exports), and `AGENTS.md` mirroring the connector pattern.
- `cmd/github/main.go` — the two-line composition root: `appkit.Main(githubapp.Spec())`.
- `internal/githubapp/spec.go` — `Spec()` returning `App:"github"`,
  `Mount:"/srv/github/"`, `Port:3203`, `MCP:true`, `Migrations: db.FS`, and a
  `Handlers` hook. **No** `Feed`, `Events`, `Producer`, or `Workers`. In this phase
  `Handlers` may mount only a placeholder (the real client/routes arrive in later
  phases); it must build green.
- `internal/db/` — `db.go` (embedded `FS` + `Open`/`Migrate` delegating to
  `appkit/db`, copied from gmail) and `migrations/001_schema_migrations.sql` (the
  bootstrap tracking table only — no domain tables).
- `etc/manifest.env` — `APP=github`, `MOUNT=/srv/github/`, `PORT=3203`, `MCP=true`,
  `DEFAULT=false`; **no** `FEED=` line, no outbox retention keys.

Do **not** wire the service into the suite in this phase: no edits to the root
`go.work`, root `bin/*`, the dashboard's nginx include, or any sibling module.
That is out-of-scope suite work.

Observable end state: `github/` is a self-contained module that builds green in
isolation and produces a binary exposing appkit's fixed verb set, declaring a
stateless non-producer service with a bootstrap-only schema.

## Done when

All hold on identical repo state, from the module root (`github/`):

- `GOWORK=off go build ./...` exits 0, and `GOWORK=off go test ./...` exits 0.
- `gofmt -l .` prints nothing and `go vet ./...` is clean.
- The built binary's `manifest` verb output contains `MCP=true` and `PORT=3203`,
  and contains **no** line matching `^FEED=` and no `OUTBOX_RETENTION`:
  `./build/github.bin manifest | grep -c -E '^FEED=|OUTBOX_RETENTION'` returns `0`.
- `internal/db/migrations/` contains exactly one `.sql` file:
  `ls github/internal/db/migrations/*.sql | wc -l` returns `1`.
- The binary responds to `version` with the contents of `VERSION`
  (exit 0).
