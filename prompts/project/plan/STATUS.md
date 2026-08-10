# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 77

- Phase 76 ⬜ realizes R-A8EU-5VUL, R-A9MQ-JNLA, R-AAUM-XFBZ, R-AC2J-B72O, R-ADAF-OYTD, R-AEIC-2QK2 — patch-semantics update and the no-empty-prompt guard
