# prompts agentkit migration — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 47

- Phase 44 ⬜ realizes R-ZZVE-DK4O, R-013A-RBVD, R-03J3-IVCR — browse queries: paged, counted, filtered list methods on the domain stores
- Phase 45 ⬜ realizes R-ZW7P-88WL, R-ZXFL-M0NA, R-ZYNH-ZSDZ, R-04QZ-WN3G, R-05YW-AEU5, R-076S-O6KU, R-08EP-1YBJ, R-09ML-FQ28, R-0AUH-THSX — the `ui/` namespace and the two list tabs; the landing card retires
- Phase 46 ⬜ realizes R-0C2E-79JM, R-0DAA-L1AB, R-0EI6-YT10, R-0FQ3-CKRP, R-0GXZ-QCIE, R-0I5W-4493, R-0JDS-HVZS — detail pages: prompt detail, the run log, and the raw-body endpoint

