# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 56

- Phase 52 ⬜ realizes D44 — the correlation id on the record: `runs.correlation_id`, `calls.correlation_id`, mint-or-inherit at spawn
- Phase 53 ⬜ realizes D47 — rebuild to adopt: event-plane chain continuation, the spawn `root` record, the recorded boundary
- Phase 54 ⬜ realizes D45 — chain-stamped peer calls via the `MCPServer` headers agentkit injects
- Phase 55 ⬜ realizes D46 — nginx fragment: capture the chain id on gated locations, strip it on the ungated one
