# Phase 12 — Promote lint tier to `strict`

*Realizes design Decision 12 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`eventplane/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict; the strict tier currently
reports these findings, all internal complexity/style items resolvable without
changing any exported signature or seam:

- gocritic `unnamedResult` in `correlation/correlation.go` (name the returned
  results).
- `gocyclo` (complexity 22 > 15) on `(*engine).run` in `consumer/consumer.go`.
- `gocyclo` (complexity 28 > 15) on `Match`, (16 > 15) on `CouldMatchSubject`,
  and (17 > 15) on `advance`, all in `routing/routing.go`.
- `nestif` (complexity 7) on the `if lid != ""` block in `outbox/feed.go`.

Reduce the flagged functions' branching/nesting by extraction so `bin/lint
eventplane` is clean at strict, and land the marker flip in the same completion
commit.

**Done when:** `cat eventplane/.lint-tier` prints exactly `strict`;
`bin/lint eventplane` (from the repo root) exits 0 reporting tier `strict`; and
the suite is green per CONVENTIONS (`go test ./...` and `go vet ./...` from
`eventplane/` exit 0, `gofmt -l .` prints nothing).
