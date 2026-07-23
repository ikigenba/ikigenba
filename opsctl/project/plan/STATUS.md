# opsctl — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** the phase's line here and its `phase-NN.md` body file — there
is no done marker; done is gone. This file deliberately carries **no bare
status glyph** outside phase lines, so the anchored grep matches only phase
lines.

Next phase: 17

- Phase 16 ⬜ realizes D10 (R-JQGB-RYA2) + D11 (R-ML75-3NVZ) — extend the init-box package baseline (tar, curl-minimal) and add the generic InstallScript seam that installs the oauth CLI to /usr/local/bin
