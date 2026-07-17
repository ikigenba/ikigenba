# appkit — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's status marker lives. Each phase line is a Markdown
bullet beginning with `- Phase` and carries `⬜` (pending) — there is no `✅`
state on disk. The build loop finds its next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, builds it, and on completion **deletes**
that phase's line here and its `phase-NN.md` file. This file deliberately
carries **no bare status glyph** anywhere but on a phase line.

Next phase: 16
