# wiki — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carries `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** the phase's line here and its `phase-NN.md` body file — there is
no done marker; done is gone. This file deliberately carries **no bare status
glyph** anywhere but on a phase line, so the anchored grep matches only phase
lines.

Next phase: 143

- Phase 141 ⬜ realizes R-4Q8B-N9SX, R-4RG8-11JM, R-4TW0-SL10 — the public tier serves ask; one Ask label on both tiers
- Phase 142 ⬜ realizes R-HN4G-06FQ, R-HOCC-DY6F — landing and selector redirects pick the tier by scope visibility

