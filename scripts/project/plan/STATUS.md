# scripts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's marker lives. Each phase line is a Markdown bullet
beginning with `- Phase` carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line and its `phase-NN.md` body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 43

- Phase 42 ⬜ realizes R-IKGJ-ND9W, R-ILOG-150L, R-IO48-SOHZ, R-IPC5-6G8O, R-IQK1-K7ZD — conform the version-plane client to repos' real surface (MCP domain verbs + plumbing byte routes)

