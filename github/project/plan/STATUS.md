# github — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and on completion **deletes**
that phase's line and its body file — there is no done marker; done is gone.
This file deliberately carries **no bare status glyph** outside phase lines, so
the anchored grep matches only phase lines.

Next phase: 20

- Phase 19 ⬜ realizes D13 — prove the adopted suite contracts (`R-4LKF-FB23`, `R-8DF1-W89F`, `R-8IAN-FB87`) at the composition root in `cmd/github/main_test.go`

