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

Next phase: 137

- Phase 136 ⬜ realizes R-HG6J-SNRN, R-HHEG-6FIC, R-HIMC-K791, R-HJU8-XYZQ, R-HL25-BQQF, R-HMA1-PIH4, R-HOPU-H1YI, R-HPXQ-UTP7 — the new page shell (boxed header bar, Home to the wiki landing, compact selector) and the suggested-pages landing replacing the orphan and browse indexes
