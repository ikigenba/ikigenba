# cron — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
`✅` marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 15

- Phase 14 ⬜ realizes R-8ALX-VK6V, R-8BTU-9BXK — forward all four owner-identity headers (`X-Owner-Id`/`X-Owner-Email`/`X-Owner-Name`/`X-Owner-Picture`) through cron's nginx fragment on both identity-forwarding locations
