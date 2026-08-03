# repos — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the
build loop **deletes** the phase's line and its body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside the phase lines, so the anchored grep matches only phase lines.

Next phase: 23

- Phase 20 ⬜ realizes D13 — nginx fragment: capture the introspection-minted correlation id on the gated locations, strip it on the ungated PRM bootstrap
- Phase 21 ⬜ realizes D11 — the github token source and MCP peer move onto the chassis's instrumented outbound HTTP client (needs appkit first)
- Phase 22 ⬜ realizes D12 — a session carries its correlation id in its row, so the outcome event stays on the chain across detached contexts and restarts (needs eventplane first)
