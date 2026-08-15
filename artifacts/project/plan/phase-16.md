# Phase 16 — Promote lint tier to `strict`

*Realizes design Decision 12 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`artifacts/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports 11 gocritic `unnamedResult` findings — in `internal/artifacts`
(`download.go`, `import.go`, `upload.go`) and `internal/mcp` (`tools.go`, eight
sites). All are the same internal style item, resolvable by naming the returned
results without changing any exported signature or seam. Fix them so
`bin/lint artifacts` is clean at strict, and land the marker flip in the same
completion commit.

**Done when:** `cat artifacts/.lint-tier` prints exactly `strict`;
`bin/lint artifacts` (from the repo root) exits 0 reporting tier `strict`; and
the suite is green per CONVENTIONS (`cd artifacts && go build ./...`,
`go vet ./...`, `gofmt -l .` empty, `go test ./...` all succeed).
