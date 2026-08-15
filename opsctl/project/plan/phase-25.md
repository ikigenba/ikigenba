# Phase 25 — Promote lint tier to `strict`

*Realizes design Decision 18 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`opsctl/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports 15 findings, all in `internal/opsctl` and all internal complexity/style
items resolvable without changing any exported signature or seam:

- `gocyclo` (cyclomatic complexity > 15) on `Restore` (`backup.go`),
  `ConvertOldLayout` (`convert.go`), `Stage`, `unpackBundle`, `Deploy`
  (`deploy.go`), `InitBox` (`initbox.go`), `Rollback` (`rollback.go`), `Setup`
  (`setup.go`), and `Teardown` (`teardown.go`) — reduce branching by extraction.
- `nestif` (complex nested blocks) in `convert.go`, `deploy.go`, `initbox.go`,
  and `teardown.go` — flatten with guard clauses / early returns.
- gocritic `unnamedResult` in `rollback.go` — name the returned results.
- gocritic `paramTypeCombine` in `seam.go` — combine same-typed adjacent params.

Fix them so `bin/lint opsctl` is clean at strict, and land the marker flip in
the same completion commit.

**Done when:** `cat opsctl/.lint-tier` prints exactly `strict`;
`bin/lint opsctl` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`GOWORK=off go build ./...` and
`GOWORK=off go test ./...` from `opsctl/` succeed).
