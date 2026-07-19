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

Next phase: 95

- Phase 94 ⬜ realizes R-1BRG-F9TN, R-1CZC-T1KC, R-1E79-6TB1, R-1GN1-YCSF, R-0UOV-2HFX, R-0VWR-G96M — the retirement sweep: llm_calls, recorder stack, eval harness, agentkit
