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

- Phase 91 ⬜ realizes R-J8QP-BETB, R-4BCC-0EHJ, R-J9YL-P6K0, R-JCEE-GQ1E, R-0X4N-U0XB, R-0ZKG-LKEP, R-10SC-ZC5E, R-1209-D3W3, R-MSKH-GPX5, R-MTSD-UHNU, R-MV0A-89EJ, R-MW86-M158 — convert the chat seam to the prompts /complete client
- Phase 92 ⬜ realizes R-Z932-H2RA, R-1385-QVMS, R-14G2-4NDH, R-15NY-IF46 — embeddings through /embed
- Phase 93 ⬜ realizes R-16VU-W6UV, R-183R-9YLK, R-19BN-NQC9, R-1AJK-1I2Y — origin and correlation attribution
- Phase 94 ⬜ realizes R-1BRG-F9TN, R-1CZC-T1KC, R-1E79-6TB1, R-1GN1-YCSF, R-0UOV-2HFX, R-0VWR-G96M — the retirement sweep: llm_calls, recorder stack, eval harness, agentkit
