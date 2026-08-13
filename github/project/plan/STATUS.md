# github — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and on completion **deletes**
that phase's line and its body file — there is no done marker; done is gone.
This file deliberately carries **no bare status glyph** outside phase lines, so
the anchored grep matches only phase lines.

Next phase: 24

- Phase 23 ⬜ realizes R-WI06-793Q, R-WJ82-L0UF, R-WKFY-YSL4, R-WLNV-CKBT, R-WMVR-QC2I, R-WO3O-43T7, R-WPBK-HVJW, R-WQJG-VNAL, R-WRRD-9F1A, R-WSZ9-N6RZ, R-WU76-0YIO — migrate github's landing + static from embedded internal/web to disk-served share/www
