# Phase 17 — Promote lint tier to `strict`

*Realizes design Decision 13 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`telemetry/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports four gocritic `unnamedResult` findings — two in `internal/db/store.go`
and two in `internal/mcp/mcp.go`. All are internal style items, resolvable
without changing any exported signature or seam (name the returned results).
Fix them so `bin/lint telemetry` is clean at strict, and land the marker flip in
the same completion commit.

**Done when:** `cat telemetry/.lint-tier` prints exactly `strict`;
`bin/lint telemetry` (from the repo root) exits 0 reporting tier `strict`; and
the suite is green per CONVENTIONS (`cd telemetry && go build ./...`,
`go vet ./...`, `go test ./...` all succeed).
