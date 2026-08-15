# Phase 178 — Promote lint tier to `strict`

*Realizes design Decision 99 (adopt the suite lint contract at tier `strict`).*

Bring the tree to zero findings at the strict tier, then create
`wiki/.lint-tier` containing exactly `strict` (one line, trailing newline
permitted) — the tree currently has no marker, so its tier is `off` and the
gate passes vacuously. The tree is **not** yet clean at strict: a strict run
reports roughly 104 findings across formatting (gofumpt), unused symbols
(unused), staticcheck simplifications, and the strict-only complexity and
judgment rules (gocyclo, dupl, nestif, funlen, and the gocritic
`unnamedResult` / `paramTypeCombine` checks). All are internal
complexity/style items, resolvable without changing any exported signature or
seam (reformat; delete or wire up the unused symbols; apply the staticcheck
rewrites; shorten/extract long or deeply-nested functions; de-duplicate; name
returned results). Fix them so `bin/lint wiki` is clean at strict, and land the
marker creation in the same completion commit.

**Done when:** `cat wiki/.lint-tier` prints exactly `strict`; `bin/lint wiki`
(from the repo root) exits 0 reporting `tier=strict findings=0`; and the suite
is green per CONVENTIONS (`cd wiki && go build ./...`, `go vet ./...`,
`gofmt -l .` empty, `go test ./...` all succeed).
