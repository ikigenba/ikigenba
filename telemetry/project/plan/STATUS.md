# telemetry — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase` followed by its zero-padded number, the marker
`⬜` (pending), then `realizes <Decision ids>` (or `realizes —` for a purely
structural phase), then `— <one cohesive objective>`. The build loop finds its
next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and on completion **deletes** that phase's
line here together with its `phase-NN.md` — there is no done marker; done is
gone. A phase body file carries no marker of its own. This document
deliberately carries **no** bare status glyph outside the phase lines, so the
anchored grep matches only phase lines.

Next phase: 08

- Phase 01 ⬜ realizes D1 — Module skeleton, chassis Spec, composition root, manifest and VERSION
- Phase 02 ⬜ realizes D2 — The record type, the schema, and the append-only store
- Phase 03 ⬜ realizes D3 — The loopback-only, never-self-recorded ingest endpoint
- Phase 04 ⬜ realizes D4 — Retention window resolution and the pruner
- Phase 05 ⬜ realizes D5 — The forensic MCP surface: search, chain, get, guide
- Phase 06 ⬜ realizes D6 — The shipped nginx location fragment and its drift guards
- Phase 07 ⬜ realizes D7 — The end-to-end layer over the real composed service
