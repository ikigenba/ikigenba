# Phase 41 — Promote lint tier to `strict`

*Realizes design Decision 32 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`dropbox/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict; the strict tier currently
reports nine findings, all internal complexity/style items resolvable without
changing any exported signature or seam:

- `funlen` — `cmd/dropbox/main.go` (`main`, 142 > 80) and
  `internal/mcp/tools.go` (`Tools`, 113 > 80).
- `gocyclo` (> 15) — `internal/dropbox/service.go` (`Move`, 17),
  `internal/dropbox/sync.go` (`steadyState`, 19), and
  `internal/mcp/tools.go` (`putSourceURL`, 21).
- `nestif` — `internal/dropbox/health.go` (19),
  `internal/dropbox/service.go` (7), and `internal/dropbox/sync.go` (5).
- gocritic `unnamedResult` — `internal/dropbox/sync.go`.

Extract helpers to shorten/flatten the long and complex functions, reduce
nesting, and name the returned results, so `bin/lint dropbox` is clean at
strict. Land the marker flip in the same completion commit.

**Done when:** `cat dropbox/.lint-tier` prints exactly `strict`;
`bin/lint dropbox` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`cd dropbox && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` all succeed).
