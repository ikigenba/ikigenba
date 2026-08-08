# opsctl — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** the phase's line here and its `phase-NN.md` body file — there
is no done marker; done is gone. This file deliberately carries **no bare
status glyph** outside phase lines, so the anchored grep matches only phase
lines.

Next phase: 24

- Phase 21 ⬜ realizes D17 (R-O1AD-MRKW, R-O2IA-0JBL) — declare opsctl's testing facts in `AGENTS.md` and prove the declaration and the no-skip rule
- Phase 22 ⬜ realizes R-O3Q6-EB2A, R-O4Y2-S2SZ (umbrella `root project/design/D05.md`, `[proof: opsctl]`) — prove the backup archive's state-only boundary against a real `tar` listing
- Phase 23 ⬜ realizes D17 (R-2B4O-Z98N; absorbs R-WRJF-H7J9, R-66UP-LI59, R-6FE0-9WC4, R-MYS7-2H2R, R-AXY7-K8GA, R-B0E0-BRXO, R-JRO8-5Q0R, R-MMF1-HFMO) — the committed manual-layer verification runbook
