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

Next phase: 178

- Phase 171 ⬜ realizes R-74CQ-9083, R-76SJ-0JPH, R-780F-EBG6, R-798B-S36V, R-7AG8-5UXK — corrections data model: kind column, suppressions table, effective claim set
- Phase 172 ⬜ realizes R-7BO4-JMO9, R-7CW0-XEEY — extract classifies statements into claims and corrections
- Phase 173 ⬜ realizes R-7FBT-OXWC, R-7K7F-80V4 — the internal/match package and its call site
- Phase 174 ⬜ realizes R-7GJQ-2PN1, R-7HRM-GHDQ, R-7IZI-U94F, R-7LFB-LSLT, R-7MN7-ZKCI, R-7NV4-DC37, R-7QAX-4VKL — the pipeline's match phase and effective-set integrate
- Phase 175 ⬜ realizes R-7RIT-INBA, R-7SQP-WF1Z, R-7TYM-A6SO, R-7V6I-NYJD — corrections end to end: reassertion, recency, re-run, merge cross-match
- Phase 176 ⬜ realizes R-7WEF-1QA2, R-7XMB-FI0R — the corrections read surface: claims labels and the guide
- Phase 177 ⬜ realizes R-7E3X-B65N, R-7YU7-T9RG, R-8024-71I5, R-81A0-KT8U — autotune: match folder, scorer, extract corrections cases
