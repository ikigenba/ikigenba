# Phase 26 — Promote lint tier to `strict`

*Realizes design Decision 23 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`webhooks/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports a gocritic `unnamedResult` finding in `internal/db` and a `gocyclo`
finding (complexity 20 > 15) in `internal/webhooks` (`NewIngressHandler`). Both
are internal complexity/style items, resolvable without changing any exported
signature or seam (name the returned results; reduce the handler's branching by
extraction). Fix them so `bin/lint webhooks` is clean at strict, and land the
marker flip in the same completion commit.

**Done when:** `cat webhooks/.lint-tier` prints exactly `strict`;
`bin/lint webhooks` (from the repo root) exits 0 reporting tier `strict`; and
the suite is green per CONVENTIONS (`cd webhooks && go build ./...`,
`go vet ./...`, `gofmt -l .` empty, `go test ./...` all succeed).
