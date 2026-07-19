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

- Phase 35 ⬜ realizes R-1X6X-E3KP, R-1ZMQ-5N23 — runner resolves the catalog route: wire model and pricing on the Conversation
- Phase 36 ⬜ realizes R-20UM-JESS, R-222I-X6JH — MCP surface: model-only required schema and catalog-generated describe inventory
