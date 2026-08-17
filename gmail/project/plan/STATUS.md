# gmail — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop deletes the phase's line and body file — there is no done marker; done is
gone. This file deliberately carries **no bare status glyph** anywhere but on a
phase line, so the anchored grep matches only phase lines.

Next phase: 33

- Phase 32 ⬜ realizes R-ENXH-KJ01, R-EP5D-YAQQ, R-S3ZK-NBWM — `GMAIL_REFRESH_TOKEN` moves to channel 5 (`state/`): client read/rotate, consent writes `state/`, `rotating` declaration

