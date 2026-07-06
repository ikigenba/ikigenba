# Phase 12 — Delete the chassis shims and true up the doctrine doc

*Realizes design Decision 14 (structural). Depends on Phases 09–11 (the
doctrine doc states the fully converted truth); sequenced last so the reference
shape lands whole.*

Observable end state: `notify/internal/db` retains only the embedded migration
set (`FS`) and the feed-offset guard tests, with the `Open`/`Migrate` wrapper
functions removed and the test harnesses calling appkit's db package directly
(no test assertion changes — harness plumbing only). `notify/AGENTS.md` (via
the `CLAUDE.md` symlink invariant — one file, edit `AGENTS.md`) describes the
converted service: the `Spec.Consumers` declaration and chassis-resolved
`NOTIFY_<SRC>_FEED_URL`/`NOTIFY_<SRC>_FROM` config, `CONSUMES=crm,prompts`,
the `share/www` web surface, and `internal/mcp` as the `send` tool table —
with the falsified claims (appkit `Worker` wiring, `CRM_FEED_URL`/
`NOTIFY_FROM`, `Consumes:["crm"]`, the stale
`internal/server`/`internal/logging`/`internal/ids` layout entries, the
embedded-assets description) purged, no archaeology.

**Done when:** the suite is green — `cd notify && go build ./...`,
`cd notify && go vet ./...`, `cd notify && gofmt -l .` (no output), and
`cd notify && go test ./...` all succeed with zero failures — and:

- `grep -n "func Open\|func Migrate" notify/internal/db/db.go` returns no
  matches, while `grep -n "go:embed migrations" notify/internal/db/db.go`
  still matches and `notify/internal/db/migrations_feed_offset_test.go` still
  exists;
- `grep -n "CRM_FEED_URL\|PROMPTS_FEED_URL\|NOTIFY_FROM" notify/AGENTS.md`
  returns no matches, and `grep -c "Consumers" notify/AGENTS.md` reports at
  least 1;
- `grep -n "internal/ids\|internal/server\|internal/logging" notify/AGENTS.md`
  returns no matches.
