# repos — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the
build loop **deletes** the phase's line and its body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside the phase lines, so the anchored grep matches only phase lines.

Next phase: 39

- Phase 29 ⬜ realizes D17, D23 (slice) — custody, the `Git` seam, the ref choke point, and the events registry
- Phase 30 ⬜ realizes D18 (slice) — the loopback read API: content, list, stat, archive
- Phase 31 ⬜ realizes D18 (slice), D23 (slice) — the loopback commit API: put, delete, batch
- Phase 32 ⬜ realizes D19 — the git smart-HTTP door
- Phase 33 ⬜ realizes D20 — run tokens
- Phase 34 ⬜ realizes D21 (slice) — statuses
- Phase 35 ⬜ realizes D21 (slice) — the merge verb
- Phase 36 ⬜ realizes D22 — the MCP tool surface (the agent and owning-service verb door)
- Phase 37 ⬜ realizes D10, D13 — the nginx fragment: the git-door location and correlation capture
- Phase 38 ⬜ realizes D1, D14, D15, D16 — composition root, suite-contract conformance, and the tree's declarations
