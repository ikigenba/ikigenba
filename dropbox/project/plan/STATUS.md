# dropbox — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜`. The build loop finds its next unit of work
with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and builds it. On completion the build loop
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
done marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 39

- Phase 38 ⬜ realizes D30 — testing-language conformance: delete the untagged live probe, hard-fail the live layer's missing credentials, declare the tree's testing facts, and add the two adopted conformance tests
