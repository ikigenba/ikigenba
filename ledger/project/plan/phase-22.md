# Phase 22 — Promote lint tier to `strict`

*Realizes design Decision 20 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`ledger/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports a `funlen` finding in `internal/mcp` and a gocritic `unnamedResult`
finding in `internal/ledger`. Both are internal complexity/style items,
resolvable without changing any exported signature or seam (shorten/extract the
long function; name the returned results). Fix them so `bin/lint ledger` is
clean at strict, and land the marker flip in the same completion commit.

**Done when:** `cat ledger/.lint-tier` prints exactly `strict`;
`bin/lint ledger` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`cd ledger && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` all succeed).
