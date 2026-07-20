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

Next phase: 97

- Phase 96 ⬜ realizes D3, D25, D10, D16, D27 (and touch-ups in D4, D26, D61, D62) — owner_email → owner_id conversion: rename the jobs/aliases owner columns to the owner_id/owner_email pair via one new migration, capture the id as the durable key with the email as a write-once snapshot, expose both in the jobs/merges MCP results
