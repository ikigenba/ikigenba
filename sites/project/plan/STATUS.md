# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 35

- Phase 34 ⬜ realizes R-Z3ZN-5BFE R-Z57J-J363 R-Z6FF-WUWS — owner-id conversion: rebuild `sites` on `owner_id`, snapshot `owner_email`, thread the owner from Identity
