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

Next phase: 169

- Phase 167 ⬜ realizes R-XJ27-56BR, R-XP5P-2118, R-6YVT-TFOD, R-MRG8-K2WP — the worker's prompts calls carry the job's chain id, not its job id
- Phase 168 ⬜ realizes R-N729-RY1I — `status` reports the job's chain handle
