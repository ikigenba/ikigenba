# Phase 60 — Promote lint tier to `strict`

*Realizes design Decision 40 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`sites/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports these findings, all internal complexity/style items resolvable without
changing any exported signature or seam —

- `funlen` — `internal/mcp/tools.go` (`toolsWithToken`, 113 > 80 lines).
- `gocyclo` — `internal/files/files.go` (`Grep`, complexity 21 > 15).
- `gocyclo` — `internal/mcp/sync.go` (`toolSync`, complexity 31 > 15).
- `gocyclo` — `internal/mcp/tools.go` (`toolSetPath`, complexity 16 > 15).
- `nestif` — `internal/files/files.go` (complex nested blocks, complexity 6).
- gocritic `unnamedResult` — `internal/files/files.go`, `internal/mcp/files.go`
  (two occurrences).

Fix them (shorten/extract long or complex functions, flatten nesting, name the
returned results) so `bin/lint sites` is clean at strict, and land the marker
flip in the same completion commit.

**Done when:** `cat sites/.lint-tier` prints exactly `strict`; `bin/lint sites`
(from the repo root) exits 0 reporting tier `strict`; and the suite is green per
CONVENTIONS (`cd sites && go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test ./...` all succeed).
