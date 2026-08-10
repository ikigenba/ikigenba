# dashboard — Plan Status (web surface & sign-in)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 52

- Phase 51 ⬜ realizes R-VN4Y-ERZ1, R-VOCU-SJPQ, R-VQSN-K374, R-VT8G-BMOI, R-VUGC-PEF7 — metrics, profile, authorize, and show-once pages adopt the shell


