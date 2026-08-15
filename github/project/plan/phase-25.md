# Phase 25 — Promote lint tier to `strict`

*Realizes design Decision 16 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`github/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports four gocritic `paramTypeCombine` findings in `internal/gh/client.go`
and one `gocyclo` finding (complexity 32 > 15) on `Tools` in
`internal/mcp/tools.go`. All are internal complexity/style items, resolvable
without changing any exported signature or seam (collapse adjacent same-typed
parameters; reduce the tool dispatcher's branching by extraction). Fix them so
`bin/lint github` is clean at strict, and land the marker flip in the same
completion commit.

**Done when:** `cat github/.lint-tier` prints exactly `strict`;
`bin/lint github` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`GOWORK=off go build ./...`,
`GOWORK=off go vet ./...`, `gofmt -l .` empty, `GOWORK=off go test ./...` all
succeed from `github/`).
