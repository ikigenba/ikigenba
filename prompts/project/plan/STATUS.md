# prompts agentkit migration — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 43

- Phase 40 ⬜ realizes R-604H-L3QC, R-61CD-YVH1, R-62KA-CN7Q, R-63S6-QEYF, R-6503-46P4, R-667Z-HYFT — `POST /embed`, the synchronous embedding endpoint
- Phase 41 ⬜ realizes R-6JMV-PFLG, R-6KUS-37C5, R-6M2O-GZ2U, R-6NAK-UQTJ, R-6B3L-11EL — sessions write `calls` rows in the FinishRun tx; runs under the run cap (D31 runner slice)
- Phase 42 ⬜ realizes R-6DJD-SKVZ, R-6ERA-6CMO, R-6FZ6-K4DD, R-6H72-XW42, R-6IEZ-BNUR — the `calls` and `usage` MCP tools
