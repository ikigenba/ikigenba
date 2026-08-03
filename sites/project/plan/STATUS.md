# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 44

- Phase 42 ⬜ realizes D29 — nginx fragment: capture the introspection-minted correlation id on the gated locations, strip it on the ungated public ones
- Phase 43 ⬜ realizes D28 — the dropbox mirror client takes the Router-provided instrumented outbound HTTP client (needs appkit first)
