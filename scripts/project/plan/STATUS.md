# scripts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line and its `phase-NN.md` body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 42

- Phase 39 ⬜ realizes D40 (slice: R-2W7Z-MXQR, R-2XFW-0PHG) — seed every existing script into the plane
- Phase 40 ⬜ realizes D40 (slice: R-2YNS-EH85) — retire the `body` column behind the seeding guard
- Phase 41 ⬜ realizes D26 (slice: R-2ZVO-S8YU) — `describe` teaches the git-backed model

