# repos — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the
build loop **deletes** the phase's line and its body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside the phase lines, so the anchored grep matches only phase lines.

Next phase: 27

- Phase 26 ⬜ realizes R-O1AD-MRKW, R-O2IA-0JBL — declare repos' testing facts in `AGENTS.md` (layers, the real-`git` and `go`-on-`PATH` preconditions, gate command, GOWORK mode) and prove the declaration plus the skip ban with two tagged tests
