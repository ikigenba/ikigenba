# crm — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each pending phase line is a
Markdown bullet beginning `- Phase` and carries `⬜`. The build loop finds its
next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and builds it. On completion the build loop
**deletes** that phase's line here and its `project/plan/phase-NN.md` body file
in the completion commit — there is no done marker; done is gone. This file
deliberately carries **no bare status glyph** anywhere but on a phase line, so
the anchored grep matches only phase lines.

Next phase: 18

