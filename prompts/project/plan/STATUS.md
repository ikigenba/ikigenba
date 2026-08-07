# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 59

- Phase 58 ⬜ realizes D21, D23, D26 (framing-prompt slices) — pin the three unproven framing-prompt claims (`R-6AUG-NHQY`, `R-6I5U-Y474`, `R-FEGC-LVD7`) against the assembled conversation `System`, and drop the retired `R-6DA9-F18C` tag
