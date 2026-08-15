# bin — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's status marker lives. Each phase line is a Markdown
bullet beginning with the literal `- Phase` and its zero-padded number, then
`⬜` (pending), then `realizes <Decision ids>` (or `realizes —` for a pure
structural phase), then `— <objective>`. The build loop finds its next work with
`grep -nE '^- Phase .* ⬜' bin/project/plan/STATUS.md | head -1`, reads only that
phase's `bin/project/plan/phase-NN.md`, and **on completion deletes that phase's
line here and its body file** in the completion commit — there is no done
marker; done is deleted, and history lives in git. This file carries no bare
status glyph outside phase lines, so the anchored grep matches only phase lines.

Next phase: 09

- Phase 07 ⬜ realizes R-WW5B-L155, R-WXD7-YSVU, R-WYL4-CKMJ, R-WZT0-QCD8, R-X10X-443X, R-X28T-HVUM, R-X3GP-VNLB, R-X4OM-9FC0 — `bin/lint` runner + `bin/lint.d/` tier configs, proven in `bin/bintest`
- Phase 08 ⬜ realizes R-X5WI-N72P — `bin/ship` refuses a tree red at its registered tier
