# crm — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's pending marker lives. Each pending phase line is a
Markdown bullet beginning `- Phase` and carries `⬜`. The build loop finds its
next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and builds it. On completion the build loop
**deletes** that phase's line here and its `project/plan/phase-NN.md` body file
in the completion commit — there is no done marker; done is gone. This file
deliberately carries **no bare status glyph** anywhere but on a phase line, so
the anchored grep matches only phase lines.

Next phase: 26

- Phase 24 ⬜ realizes R-9LST-2NUQ, R-9N0P-GFLF, R-9O8L-U7C4, R-9PGI-7Z2T — replace the contact-lifecycle and deal-stage vocabularies via row-preserving rebuild migrations
- Phase 25 ⬜ realizes R-9QOE-LQTI, R-9RWA-ZIK7, R-9T47-DAAW, R-9UC3-R21L, R-9VK0-4TSA, R-9XZS-WD9O — add the contact_tokens table, the mint verb, and the search token lookup

