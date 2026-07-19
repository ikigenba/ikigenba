# github — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and on completion **deletes**
that phase's line and its body file — there is no done marker; done is gone.
This file deliberately carries **no bare status glyph** outside phase lines, so
the anchored grep matches only phase lines.

Next phase: 16

- Phase 15 ⬜ realizes R-1GOK-GA2F, R-1HWG-U1T4 — forward all four owner headers through the nginx fragment
