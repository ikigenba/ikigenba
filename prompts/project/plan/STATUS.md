# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 61

- Phase 60 ⬜ realizes R-O1AD-MRKW, R-O2IA-0JBL — declare prompts' testing facts in `AGENTS.md` (default gate, hermetic + composed layers, no preconditions, GOWORK mode) and prove the declaration and the no-skip rule from `cmd/prompts/main_test.go`
