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

Next phase: 124

- Phase 121 ⬜ realizes D65 (storage + minting slice) — the ingest job row stores the chain id of the request that enqueued it and the worker resumes it, while self-started work roots its own: one per job with no stored id, one per catch-up-sweep drain cycle
- Phase 122 ⬜ realizes D64 and D65's R-KIH2-R4UC — the prompts calls move onto the chassis's shared instrumented client and carry the received chain id, retiring the per-ask mint and the job-id-as-correlation rule
- Phase 123 ⬜ realizes — — nginx fragment: forward the edge-minted correlation id on the four gated locations, strip it to empty on the ungated PRM bootstrap
