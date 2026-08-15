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

Next phase: 178

- Phase 176 ⬜ realizes R-7WEF-1QA2, R-7XMB-FI0R — the corrections read surface: claims labels and the guide
- Phase 177 ⬜ realizes R-7E3X-B65N, R-7YU7-T9RG, R-8024-71I5, R-81A0-KT8U — autotune: match folder, scorer, extract corrections cases
