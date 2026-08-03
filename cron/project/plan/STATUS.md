# cron — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
`✅` marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 18

- Phase 16 ⬜ realizes D16 (fragment slice: R-BBON-OZTN, R-BE4G-GJB1) — carry `X-Correlation-Id` across cron's nginx trust boundary: capture on every gated location, strip on the ungated PRM bootstrap
- Phase 17 ⬜ realizes D16 (code slice) — mint a root correlation id at every tick via the chassis `StartRoot`, record its `root` with op `cron:tick/<name>`, and add the `correlation_id` column by one additive migration
