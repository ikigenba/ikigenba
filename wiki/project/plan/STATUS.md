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

Next phase: 146

- Phase 144 ⬜ realizes R-NC9Q-4F8K, R-NDHM-I6Z9, R-NEPI-VYPY — retrieval recall fixes: porter-stemmed FTS index, space-joined keyword routing, caller-limit honor
- Phase 145 ⬜ realizes R-NFXF-9QGN, R-NH5B-NI7C, R-NID8-19Y1 — ask gathers under a body-byte budget instead of a fixed page count

