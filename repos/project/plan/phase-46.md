# Phase 46 — Promote lint tier to `strict`

*Realizes design Decision 28 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`repos/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports five `gocyclo` findings (cyclomatic complexity > 15), all internal, none
touching an exported signature or seam:

- `cmd/repos/spec.go` — `reposSpec` (18)
- `internal/repos/git_door.go` — `(*Service).serveGit` (16)
- `internal/repos/merge.go` — `(*Service).Merge` (22)
- `internal/repos/runtoken.go` — `RunTokenHandler` (17)
- `internal/repos/write.go` — `(*Service).commitChanges` (38)

Reduce each function's branching (extract helpers, collapse decision logic) so
`bin/lint repos` is clean at strict, then land the marker flip in the same
completion commit.

**Done when:** `cat repos/.lint-tier` prints exactly `strict`; `bin/lint repos`
(from the repo root) exits 0 reporting tier `strict`; and the suite is green per
CONVENTIONS (`cd repos && go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test ./...` all succeed).
