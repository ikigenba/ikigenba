# scripts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line and its `phase-NN.md` body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 31

- Phase 27 ⬜ realizes D31 — correlation lines in the committed nginx fragment (capture on the gated tiers, strip on the ungated one)
- Phase 28 ⬜ realizes D29 — the run carries its causal chain: `runs.correlation_id` end to end (needs `eventplane/correlation` + appkit's middleware built first)
- Phase 29 ⬜ realizes D30 — the chain crosses the sandbox boundary on every `suite.*` call
- Phase 30 ⬜ realizes D32 — rebuild to adopt: the chain across the consumer fan-out, out through `Append`, and the origin at spawn (needs the revised appkit + eventplane built first)

