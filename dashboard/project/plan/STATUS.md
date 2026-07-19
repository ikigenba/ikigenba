# dashboard — Plan Status (web pages restructure)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 34

- Phase 32 ⬜ realizes R-HYL8-V30T, R-HZT5-8URI, R-I111-MMI7 — `owner_id` foreign keys: rebuild the four auth-artifact carriers with `REFERENCES identities(id)`
- Phase 33 ⬜ realizes R-VULR-MABI, R-VX1K-DTSW, R-VY9G-RLJL, R-HW5G-3JJF, R-HSHQ-Y8BC, R-HTPN-C021, R-HUXJ-PRSQ, R-VZHD-5DAA, R-HXDC-HBA4 — fail-closed introspection: all four identity headers on every allow, sourced from the identities row


