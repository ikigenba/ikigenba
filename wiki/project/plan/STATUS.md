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

Next phase: 106

- Phase 103 ⬜ realizes R-ETV6-57CQ, R-EWAY-WQU4, R-EXIV-AIKT — runner widening: per-case progress, full provider set, subscription auth
- Phase 104 ⬜ realizes R-EYQR-OABI, R-EZYO-2227, R-F2EG-TLJL, R-F16K-FTSW — autotune driver core: CLI, resolved config, workspace lifecycle
- Phase 105 ⬜ realizes R-F3MD-7DAA, R-F4U9-L50Z, R-F625-YWRO, R-F7A2-COID — wrapped loop + finalizer; improve.md/README/gitignore rewritten for the driver
