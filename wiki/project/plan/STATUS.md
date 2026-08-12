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

Next phase: 167

- Phase 162 ⬜ realizes D93 — serialize ingest admission behind a durable per-scope lease
- Phase 163 ⬜ realizes D4 (identity slice) — stage plans by name; mint subject ids inside the integrate commit
- Phase 164 ⬜ realizes D4 (apply slice) — ensure only unstaged units, one transaction, job-handle attribution
- Phase 165 ⬜ realizes D94 — the inbox drain contract: no item may stop the drain
- Phase 166 ⬜ realizes D95 — liveness, the five-status surface, and wedge visibility
