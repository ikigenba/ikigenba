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

Next phase: 119

- Phase 113 ⬜ realizes R-A0XE-WA4H, R-9ZPI-IIDS, R-A25B-A1V6 — ask call sites: rehoused analysis/synthesis prompts, system/user split, luna defaults everywhere
- Phase 114 ⬜ realizes R-AAOL-YG21, R-ABWI-C7SQ — tune folders: extract and analysis, with their deterministic scorers
- Phase 115 ⬜ realizes R-A3D7-NTLV, R-A4L4-1LCK — remove the tuning machinery (executables, eval tree, agentkit, compat fields, env/gitignore rewire)
- Phase 116 ⬜ realizes R-AD4E-PZJF, R-AFK7-HJ0T — tune folder: compile, with its hybrid scorer and seed corpus
- Phase 117 ⬜ realizes R-A5T0-FD39, R-A88T-6WKN, R-A9GP-KOBC, R-AECB-3RA4, R-AGS3-VARI — tune folder: synthesis, completing the four-folder workspace
- Phase 118 ⬜ realizes — — readiness proof: one small baseline run per folder, outputs discarded
