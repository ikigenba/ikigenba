# dashboard — Plan Status (web surface & sign-in)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 43

- Phase 42 ⬜ realizes D6 — give `R-DB16-DOCS` a real test home in `cmd/dashboard/docs_test.go` and clear the orphan `R-K5RY-83NL` tag from `internal/ratelimit/ratelimit_test.go`

