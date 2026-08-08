# cron — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
`✅` marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 19

- Phase 18 ⬜ realizes R-O1AD-MRKW, R-O2IA-0JBL — declare cron's testing facts in `AGENTS.md` (default gate, hermetic + composed layers, no preconditions, GOWORK mode) and prove the declaration and the no-skip rule from `cmd/cron/main_test.go`
