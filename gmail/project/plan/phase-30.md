# Phase 30 — Promote lint tier to `strict`

*Realizes design Decision 27 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`gmail/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports five findings, all internal complexity/style items resolvable without
changing any exported signature or seam —

- `funlen`: `cmd/consent/main.go` (`run`, 88 > 80).
- `funlen`: `internal/mcp/tools.go` (`Tools`, 160 > 80).
- gocritic `unnamedResult`: `internal/gmail/sync.go` (name the returned results).
- `gocyclo`: `internal/gmail/client.go` (`(*Client).rpcCall`, 16 > 15).
- `gocyclo`: `internal/gmail/sync.go` (`(*Engine).drain`, 19 > 15).

Fix them (shorten/extract the long functions, name the results, reduce the
handlers' branching) so `bin/lint gmail` is clean at strict, and land the marker
flip in the same completion commit.

**Done when:** `cat gmail/.lint-tier` prints exactly `strict`; `bin/lint gmail`
(from the repo root) exits 0 reporting tier `strict`; and the suite is green per
CONVENTIONS (`cd gmail && go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test ./...` all succeed).
