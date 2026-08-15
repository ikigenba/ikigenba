# Phase 84 — Promote lint tier to `strict`

*Realizes design Decision 64 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then rewrite
`prompts/.lint-tier` to contain exactly `strict` (one line, trailing newline
permitted). The tree is **not** yet clean at strict: the strict tier currently
reports 25 findings — 13 `gocyclo` (functions above complexity 15), 7 gocritic
`unnamedResult`, and 5 `nestif` (over-nested conditionals) — concentrated in
`internal/prompt` (13) with the remainder spread across `cmd/prompts`,
`internal/completion`, `internal/runner`, `internal/calls`, `internal/inference`,
`internal/mcp`, `internal/tools`, and `internal/version`. All are internal
complexity/style items, resolvable without changing any exported signature or
seam (name returned results; reduce complexity and nesting by extraction). Fix
them so `bin/lint prompts` is clean at strict, and land the marker flip in the
same completion commit.

Because this is a large batch, it may take more than one build turn: each turn
fixes a slice of findings and leaves the suite green, and the marker flip happens
only in the turn that reaches zero strict findings.

**Done when:** `cat prompts/.lint-tier` prints exactly `strict`;
`bin/lint prompts` (from the repo root) exits 0 reporting tier `strict`; and the
suite is green per CONVENTIONS (`cd prompts && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, and `go test ./...` passes with no race violations).
