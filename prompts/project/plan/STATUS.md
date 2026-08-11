# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 79

- Phase 78 ⬜ realizes R-J7QG-FRDY, R-J8YC-TJ4N, R-JA69-7AVC, R-JJXG-9GSW, R-JL5C-N8JL, R-JMD9-10AA, R-JQ0Y-6BID, R-JR8U-K392, R-JSGQ-XUZR — completion-queue HTTP surface; synchronous /complete expunged
