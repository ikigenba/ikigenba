# wiki — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carries `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** the phase's line here and its `phase-NN.md` body file — there is
no done marker; done is gone. This file deliberately carries **no bare status
glyph** anywhere but on a phase line, so the anchored grep matches only phase
lines.

Next phase: 157

- Phase 153 ⬜ realizes R-R7IF-IID7, R-R8QB-WA3W, R-R9Y8-A1UL, R-RB64-NTLA — orphan-free page embeddings: guarded writes, joined hydration, sweep reaping
- Phase 154 ⬜ realizes R-RDLX-FD2O, R-RETT-T4TD, R-RG1Q-6WK2, R-RH9M-KOAR, R-RIHI-YG1G — ask retrieval honesty: drop stale hits, pin inside the scope wall
- Phase 155 ⬜ realizes R-RJPF-C7S5, R-RKXB-PZIU — the dead-job discard: a missing job row integrates nothing, cleanly
- Phase 156 ⬜ realizes R-RM58-3R9J, R-RND4-HJ08 — the honest web ask failure: styled error page, logged cause

