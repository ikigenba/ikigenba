# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 56

- Phase 50 ⬜ realizes D33, D32 (slice) — the write path: mutating file tools commit through repos, then apply to the copy
- Phase 51 ⬜ realizes D34 — `sync` reconciles as one batch commit, then updates the copy
- Phase 52 ⬜ realizes D36 — site lifecycle against the plane: create, delete (archive), slug rotation
- Phase 53 ⬜ realizes D35 — the `repos:push` materializer: re-materialize on `main`, ignore branches, skip unknown slugs, refuse bad exports
- Phase 54 ⬜ realizes D37 — the additive, re-runnable seeding pass for pre-plane sites
- Phase 55 ⬜ realizes D13 (slice), D32 (slice) — compose the plane at the root: client, `repos` consumer, background seeding, doc truth
