# appkit — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's status marker lives. Each phase line is a Markdown
bullet beginning with `- Phase` and carries `⬜` (pending) — there is no `✅`
state on disk. The build loop finds its next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, builds it, and on completion **deletes**
that phase's line here and its `phase-NN.md` file. This file deliberately
carries **no bare status glyph** anywhere but on a phase line.

Next phase: 17

- Phase 16 ⬜ realizes R-DDVL-DPVB, R-DF3H-RHM0, R-DGBE-59CP, R-DHJA-J13E, R-DIR6-WSU3 — Identity keys on `X-Owner-Id`: widened `server.Identity`, hard gate flip, id-carrying transport and health
