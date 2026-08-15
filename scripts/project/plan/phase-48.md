# Phase 48 — Promote lint tier to `strict`

*Realizes design Decision 43 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`scripts/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports these findings, all internal complexity/style items resolvable without
changing any exported signature or seam —

- `funlen` — `Tools` (114 > 80) in `internal/mcp/tools.go`
- `gocyclo` — `(*toolHandlers).dispatchTool` (complexity 49 > 15) in `internal/mcp/tools.go`
- `gocyclo` — `(*Runner).execute` (complexity 19 > 15) in `internal/runner/runner.go`
- `gocyclo` — `(*Service).Update` (complexity 16 > 15) in `internal/script/service.go`
- `nestif` — complex nested blocks (complexity 6) in `internal/script/service.go`
- gocritic `unnamedResult` in `internal/repos/client.go`

Fix them (shorten/extract long or complex functions, flatten the nested block,
name the returned results) so `bin/lint scripts` is clean at strict, and land
the marker flip in the same completion commit.

**Done when:** `cat scripts/.lint-tier` prints exactly `strict`;
`bin/lint scripts` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`cd scripts && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` all succeed).
