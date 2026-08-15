# Phase 22 — Promote lint tier to `strict`

*Realizes design Decision 23 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`crm/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict; the strict tier currently
reports six findings, all internal complexity/style items resolvable without
changing any exported signature or seam:

- `funlen` — `internal/mcp/tools.go` (`Tools`, 81 > 80 lines).
- gocritic `unnamedResult` — `internal/crm/interaction.go`, `internal/crm/store.go`,
  `internal/mcp/tools.go` (name the returned results).
- `gocyclo` — `internal/crm/contact.go` (`contactUpdate`, 17 > 15) and
  `internal/crm/service.go` (`decodeContact`, 17 > 15) (reduce branching by
  extraction).

Fix them so `bin/lint crm` is clean at strict, and land the marker flip in the
same completion commit.

**Done when:** `cat crm/.lint-tier` prints exactly `strict`; `bin/lint crm`
(from the repo root) exits 0 reporting tier `strict`; and the suite is green per
CONVENTIONS (`cd crm && go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test ./...` all succeed).
