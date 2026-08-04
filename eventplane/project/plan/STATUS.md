# eventplane — Plan Status

One line per **pending** phase in build order; this file is the only place a
phase's `⬜` marker lives. Each phase line is a Markdown bullet beginning with
`- Phase` carrying `⬜` (pending). The build loop finds its next work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` and reads only
that phase's body file. On completion the build loop deletes the phase's line
and its `phase-NN.md` body file in the completion commit — there is no `✅`
marker; done is gone. No bare status glyph appears outside phase lines, so the
anchored grep matches only phase lines.

Next phase: 10

- Phase 07 ⬜ realizes D7 — correlation on the producer path: outbox column, upgrade constant, envelope field, ctx-bearing `Append`
- Phase 08 ⬜ realizes D8 — correlation on the consumer path: the chain enters the handler's context, root minted when absent
- Phase 09 ⬜ realizes D9 — the `observe` package and its hook on both the publish and consume paths
