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

Next phase: 102

- Phase 100 ⬜ realizes R-KY7O-D7JB, R-KZFK-QZA0, R-L0NH-4R0P, R-L1VD-IIRE, R-L339-WAI3, R-L4B6-A28S, R-L5J2-NTZH — the eval runner (`cmd/eval-extract`) over agentkit
- Phase 101 ⬜ realizes — — the improvement-loop assets (`improve.md`, operator README)
