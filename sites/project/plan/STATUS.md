# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 67

- Phase 63 ⬜ realizes R-FJYN-3U1U, R-FL6J-HLSJ, R-FMEF-VDJ8 — the shared in-place reconcile becomes file/directory type-change-safe and deletes before it writes (D42)
- Phase 64 ⬜ realizes R-FNMC-959X — the interactive write path refuses a wrong-typed path up front (D33)
- Phase 65 ⬜ realizes R-FOU8-MX0M, R-FQ25-0ORB, R-FRA1-EGI0, R-FSHX-S88P, R-FTPU-5ZZE, R-FUXQ-JRQ3, R-FW5M-XJGS, R-FXDJ-BB7H, R-FYLF-P2Y6 — the `file_delete` and `rmdir` tools and their surface (D41; D13/D20/D21/D25/D38 count, schema, and prefix updates)
- Phase 66 ⬜ realizes R-G118-GMFK — `sync` enumeration skips non-file mirror entries (D34)
