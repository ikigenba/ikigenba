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

Next phase: 160

- Phase 157 ⬜ realizes R-JW4G-367U, R-JXCC-GXYJ, R-JYK8-UPP8, R-JZS5-8HFX, R-K101-M96M, R-K27Y-00XB, R-K3FU-DSO0, R-K4NQ-RKEP — `internal/llm` becomes the completion-queue client
- Phase 158 ⬜ realizes R-K73J-J3W3, R-K8BF-WVMS, R-K9JC-ANDH, R-KAR8-OF46, R-KBZ5-26UV, R-KEEX-TQC9, R-KFMU-7I2Y, R-KGUQ-L9TN, R-KI2M-Z1KC — ingest pipeline split: handoff, inbox applier, staging, `waiting`
- Phase 159 ⬜ realizes R-KJAJ-CTB1, R-KKIF-QL1Q — ask on the live queue path; write-deadline clearing
