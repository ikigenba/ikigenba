# eventplane — Plan Status

One line per **pending** phase in build order; this file is the only place a
phase's `⬜` marker lives. Each phase line is a Markdown bullet beginning with
`- Phase` carrying `⬜` (pending). The build loop finds its next work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` and reads only
that phase's body file. On completion the build loop deletes the phase's line
and its `phase-NN.md` body file in the completion commit — there is no `✅`
marker; done is gone. No bare status glyph appears outside phase lines, so the
anchored grep matches only phase lines.

Next phase: 06
