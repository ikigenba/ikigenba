# registry — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's status marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending) — pending is the only
marker this file carries, since a done phase's line is deleted, never flipped.
The build loop finds its next work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and on completion **deletes** that phase's
line and its `phase-NN.md` body file in the completion commit — there is no
`✅` marker; done is gone. This file deliberately carries **no bare status
glyph** outside phase lines, so the anchored grep matches only phase lines.

Next phase: 04
