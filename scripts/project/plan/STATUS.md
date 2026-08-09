# scripts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line and its `phase-NN.md` body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 45

- Phase 43 ⬜ realizes R-NGXY-11YC, R-NJDQ-SLFQ, R-NFQ1-NA7N — outbox schema convergence (restore frozen body, `outbox_correlation` rebuild migration, name-order drift guard, `migrations.sha256` manifest)
- Phase 44 ⬜ realizes R-NKLN-6D6F — the trigger envelope: `EVENT_JSON`/stdin carry `{source, kind, subject, event_id, payload}`, one event shape suite-wide
