# Suite operations — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's status marker lives. Each phase line is a Markdown
bullet beginning with the literal `- Phase` and its zero-padded number, then
`⬜` (pending), then `realizes <Decision ids>` (or `realizes —` for a pure
structural phase), then `— <objective>`. The build loop finds its next work
with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only
that phase's `project/plan/phase-NN.md`, and **on completion deletes that
phase's line here and its body file** in the completion commit — there is no
done marker; done is deleted, and history lives in git. This file carries no
bare status glyph outside phase lines, so the anchored grep matches only
phase lines.

Next phase: 50

- Phase 49  ⬜  realizes D12  — retire the shared-blob era (delete per-service `bin/secrets`, rewrite the seeding SOP)
