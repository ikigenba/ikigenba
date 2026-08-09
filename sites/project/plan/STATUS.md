# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 58

- Phase 56 ⬜ realizes R-3SOP-GMQL, R-3TWL-UEHA, R-42FW-ISO5 — publish root, domain half: the `path` column + migration, `ValidatePath`/`Subtree`, and the mapped push materializer
- Phase 57 ⬜ realizes R-3V4I-867Z, R-3WCE-LXYO, R-3XKA-ZPPD, R-3YS7-DHG2, R-4003-R96R, R-4180-50XG, R-43NS-WKEU, R-44VP-AC5J (updating rewritten pins R-Z8DD-BL71, R-CW5E-T20N, R-CXDB-6TRC, R-CYL7-KLI1, R-0A69-6H6K) — publish root, surface half: `create(path?)`, `set_path`, the mapped write path/`sync`, projection + schemas, guide example
