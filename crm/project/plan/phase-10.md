# Phase 10 — Delete the chassis shims (`internal/ids`, `internal/db` wrappers)

*Realizes design Decision 14 (structural). Depends on no earlier phase
mechanically; sequenced last so the reference shape lands whole.*

Observable end state: `crm/internal/ids/` is deleted and every former call site
imports `appkit/logging` and calls `logging.NewULID()`; `crm/internal/db`
retains only the embedded migration set (`FS`) and the outbox byte-equality
guard test, with the `Open`/`Migrate` wrapper functions removed and the
domain/MCP test harnesses calling `appkit/db` directly. No test assertion
changes — harness plumbing only.

**Done when:** the suite is green — `cd crm && go build ./...`,
`cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output), and
`cd crm && go test ./...` all succeed with zero failures — and:

- `grep -rn "crm/internal/ids" crm --include=*.go` returns no matches and
  `ls crm/internal/ids 2>/dev/null` reports no such directory;
- `grep -n "func Open\|func Migrate" crm/internal/db/db.go` returns no matches
  while `grep -n "go:embed migrations" crm/internal/db/db.go` still matches and
  `crm/internal/db/migrations_outbox_test.go` still exists.
