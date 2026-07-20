# scripts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line and its `phase-NN.md` body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 27

- Phase 26 ⬜ realizes D28 (owner-id keying) + D22/D21/D19/D17 revisions — rebuild the `scripts` table on `owner_id`, rekey all scoping/`ownsScript` on the id with `owner_email` as a write-once snapshot, expose both owner fields, and assert `suite.mcp` identity by id
