# dropbox — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜`. The build loop finds its next unit of work
with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and builds it. On completion the build loop
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
done marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 35

- Phase 34 ⬜ realizes R-QJ8F-AXWP — tag the composition root's own-port behavior with its missing test
