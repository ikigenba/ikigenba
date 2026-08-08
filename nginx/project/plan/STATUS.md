# nginx — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each phase line is a Markdown
bullet beginning with `- Phase` followed by its zero-padded number, the marker
`⬜` (pending), then `realizes <Decision ids>` (or `realizes —` for a purely
structural phase), then `— <one cohesive objective>`. The next unit of work is
found with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reading
only that phase's `project/plan/phase-NN.md`; on completion that line is
**deleted** together with its `phase-NN.md` — there is no done marker; done is
gone. A phase body file carries no marker of its own. This document deliberately
carries **no** bare status glyph outside the phase lines, so the anchored grep
matches only phase lines.

Next phase: 02

- Phase 01 ⬜ realizes — (structural; D4) — create `nginx/AGENTS.md` carrying the tree's manual-only testing declaration

The artifacts of D1, D2, and D3 already exist in the tree — this spec was written
over working code, not ahead of it — so those Decisions queue no work. New work
appends a `phase-NN.md` and its line here, taking its number from the counter
above.
