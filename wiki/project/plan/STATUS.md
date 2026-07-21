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

Next phase: 112

- Phase 109 ⬜ realizes R-BKSN-3IXK, R-BM0J-HAO9, R-BN8F-V2EY, R-BOGC-8U5N, R-BPO8-MLWC, R-BQW5-0DN1 — the analysis scorer: `internal/eval` config, gold, and list alignment
- Phase 110 ⬜ realizes R-BTBX-RX4F, R-BUJU-5OV4, R-BVRQ-JGLT, R-BWZM-X8CI, R-BY7J-B037 — the sibling runner: `cmd/eval-analysis` over shared agentkit plumbing
- Phase 111 ⬜ realizes R-BZFF-ORTW — the driver's second step: `autotune analysis`, loop assets, and the seed gold
