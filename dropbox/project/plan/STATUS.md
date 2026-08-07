# dropbox — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜`. The build loop finds its next unit of work
with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and builds it. On completion the build loop
**deletes** the phase's line here and its `phase-NN.md` body file — there is no
done marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 37

- Phase 36 ⬜ realizes — — remove the stray R-4LKF-FB23 tag from `TestDefaultMirrorPathTracksDurableStateDB` in `cmd/dropbox/main_test.go` (proof of record stays `TestDropboxBootsFromOpsctlLayoutAndServesHealth`)
