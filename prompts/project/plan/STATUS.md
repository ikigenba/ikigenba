# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 48

- Phase 47 ⬜ realizes R-JTBA-4RDB, R-SVPV-O479, R-SWXS-1VXY, R-SY5O-FNON, R-SZDK-TFFC, R-T0LH-7761, R-T1TD-KYWQ, R-T319-YQNF, R-T496-CIE4, R-T6OZ-41VI, R-T7WV-HTM7 — OpenAI subscription auth: the `auth` config key end to end
