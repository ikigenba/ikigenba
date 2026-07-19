# repos — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the
build loop **deletes** the phase's line and its body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside the phase lines, so the anchored grep matches only phase lines.

Next phase: 18

- Phase 17 ⬜ realizes R-UZVS-S08C, R-V13P-5RZ1 — forward all four owner identity headers (X-Owner-Id/Email/Name/Picture) through the repos nginx fragment's session-landing and bearer-prefix locations
