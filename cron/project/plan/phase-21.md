# Phase 21 — Promote lint tier to `strict`

*Realizes design Decision 19 (adopt the suite lint contract at tier `strict`).*

A structural phase: rewrite `cron/.lint-tier` to contain exactly `strict`
(one line, trailing newline permitted). The tree is already clean at the strict
tier, so no code change is expected; if the pinned linter surfaces a finding
anyway, fix it as part of this phase — the marker flip and a clean run land
together in the completion commit.

**Done when:** `cat cron/.lint-tier` prints exactly `strict`; `bin/lint cron`
(from the repo root) exits 0 reporting tier `strict`; and the suite is green per
CONVENTIONS (`cd cron && go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test ./...` pass).
