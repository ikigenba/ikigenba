# prompts agentkit migration — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 37

- Phase 34 ⬜ realizes R-1ONM-PPDU, R-1PVJ-3H4J, R-1R3F-H8V8, R-1SBB-V0LX, R-1TJ8-8SCM, R-1UR4-MK3B, R-1VZ1-0BU0 — adopt agentkit v0.6.0: module bump, typed credentials, catalog-backed validation
- Phase 35 ⬜ realizes R-1X6X-E3KP, R-1ZMQ-5N23 — runner resolves the catalog route: wire model and pricing on the Conversation
- Phase 36 ⬜ realizes R-20UM-JESS, R-222I-X6JH — MCP surface: model-only required schema and catalog-generated describe inventory
