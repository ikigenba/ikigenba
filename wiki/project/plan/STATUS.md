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

Next phase: 132

- Phase 128 ⬜ realizes R-H8HN-EXJE, R-H9PJ-SPA3, R-HAXG-6H0S, R-HC5C-K8RH, R-HDD8-Y0I6, R-HEL5-BS8V, R-HFT1-PJZK, R-HH0Y-3BQ9, R-HI8U-H3GY, R-HJGQ-UV7N (updating edited R-MUQ4-K1JS, R-YF06-03HO) — scope on the MCP surface: required parameter + management verbs
- Phase 129 ⬜ realizes R-HKON-8MYC, R-HLWJ-MEP1, R-HN4G-06FQ, R-HOCC-DY6F, R-HQS5-5HNT, R-HS01-J9EI, R-HT7X-X157, R-I0JC-7NLD — the scoped web routes: tiers, cookie redirect, selector, per-visibility URLs
- Phase 130 ⬜ realizes R-HUFU-ASVW, R-HVNQ-OKML, R-HWVN-2CDA, R-HY3J-G43Z, R-HZBF-TVUO — the public tier's read-only states: browse, keyword search, no inference
- Phase 131 ⬜ realizes — — nginx: the two scope-tier locations (structural)
