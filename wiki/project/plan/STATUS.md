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

Next phase: 150

- Phase 146 ⬜ realizes R-8TAF-XBSM, R-8UIC-B3JB — conditional relative-time anchoring in the extract prompt (+ tune copy and narrative dev case)
- Phase 147 ⬜ realizes R-8FVJ-PUMZ, R-8H3G-3MDO, R-8JJ8-V5V2, R-8KR5-8XLR — scope instructions storage, cap, and the system composer
- Phase 148 ⬜ realizes R-8LZ1-MPCG, R-8N6Y-0H35, R-8OEU-E8TU — inject scope instructions into the four inference stages
- Phase 149 ⬜ realizes R-8PMQ-S0KJ, R-8QUN-5SB8, R-8S2J-JK1X — the `instructions` MCP verb, guide, and count supersessions

