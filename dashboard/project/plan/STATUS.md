# dashboard — Plan Status (web surface & sign-in)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 42

- Phase 39 ⬜ realizes D6, D14, D16 — rename the box-health surface from "telemetry" to "metrics", end to end
- Phase 40 ⬜ realizes D30, D33 — mint the suite's correlation id at the introspection edge, and fix the apex nginx fragment
- Phase 41 ⬜ realizes D31, D32 — emit an `edge` telemetry record for every gated auth decision

