# dashboard — Plan Status (web pages restructure)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 31

- Phase 30 ⬜ realizes D7 (R-JA3I-IY1F, R-JCJB-AHIT, R-JDR7-O99I, R-JEZ4-2107, R-JG70-FSQW, R-JHEW-TKHL, R-O7K1-XEN7, R-DB19-LAND) — rebuild the login page composition around a brand title and a borderless etymology table
