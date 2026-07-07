# Phase 11 — Delete the chassis shim and true up the doctrine doc

*Realizes design Decision 13 (structural). Depends on phases 7–10 (the doctrine
doc states the fully converted truth); sequenced last so the reference shape lands
whole.*

Observable end state: `dropbox/internal/db` retains only the embedded migration
set (`migrationsFS`/`FS`) and the two guard tests (`migrations_load_test.go`,
`migrations_outbox_test.go`), with the `Open`/`Migrate` wrapper functions removed
(and the `context`/`database/sql`/`appkit/db` imports they alone used). The test
harnesses that called `db.Migrate` — `internal/dropbox/store_test.go` and
`internal/mcp/tools_test.go` — call appkit's db package directly
(`appkitdb.LoadMigrations(db.FS, "migrations")` + `appkitdb.Migrate` / `appkitdb.Open`),
a harness-plumbing swap with no test assertion changes.

`dropbox/CLAUDE.md` (the service's doctrine doc — a regular file; dropbox carries
no separate `AGENTS.md`) describes the converted service: `internal/mcp` as the
`list`+`get` tool table over `appkit/mcp` with chassis-registered
`health`/`reflection` (not an in-package JSON-RPC transport); `internal/db` as the
embedded migration set + guards only (appkit owns open + the runner via
`Spec.Migrations`); the human landing page and `static/` assets shipping on disk
under `share/www/` through `Spec.WWW`; and the loopback port resolved via
`registry.MustPort("dropbox")`. The falsified claims are purged: the
"JSON-RPC 2.0 transport" framing, the `internal/db` "SQLite open + migration
runner" line, the stale `internal/server`/`internal/logging`/`internal/ids` layout
entries, and the hardcoded-`3200` framing — no archaeology. The daemon/producer
model, the sync engine and its correctness rules, `/feed`, the `/content`/`/list`
byte routes, the events, the secrets story, and the no-backup decision are left
untouched.

**Done when:** the suite is green — `cd dropbox && go build ./...`,
`cd dropbox && go vet ./...`, `cd dropbox && gofmt -l .` (no output),
`cd dropbox && go test ./...`, and `bin/check-migrations dropbox` all succeed with
zero failures — and:

- `grep -n "func Open\|func Migrate" dropbox/internal/db/db.go` returns no matches,
  while `grep -n "go:embed migrations" dropbox/internal/db/db.go` still matches and
  both `dropbox/internal/db/migrations_load_test.go` and
  `dropbox/internal/db/migrations_outbox_test.go` still exist;
- `grep -rn "db\.Migrate\b" dropbox --include=*.go` returns no matches;
- `grep -n "internal/server\|internal/logging\|internal/ids" dropbox/CLAUDE.md`
  returns no matches, and `grep -n "JSON-RPC 2.0 transport" dropbox/CLAUDE.md`
  returns no matches;
- `grep -n "share/www" dropbox/CLAUDE.md` and
  `grep -n "registry.MustPort" dropbox/CLAUDE.md` each return at least one match.
