# dashboard — Plan Status (web surface & sign-in)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 39

- Phase 35 ⬜ realizes R-IBM4-4NJ8, R-ICU0-IF9X (D26 slice) — provider-bound handshakes in `oauth_state`
- Phase 36 ⬜ realizes R-IGHP-NQI0, R-IHPM-1I8P, R-IIXI-F9ZE, R-IK5E-T1Q3, R-ILDB-6TGS, R-IML7-KL7H (D27 + amended D7) — the `/login` chooser, provider start routes, two-CTA composition
- Phase 37 ⬜ realizes R-INT3-YCY6, R-IP10-C4OV, R-IQ8W-PWFK, R-IRGT-3O69, R-ISOP-HFWY, R-ITWL-V7NN, R-IF9T-9YRB, R-IAE7-QVSJ (D28 + D26/D25 slices) — the GitHub callback, federation gate, and wiring
- Phase 38 ⬜ realizes R-IWCE-MR51, R-IXKB-0IVQ, R-IYS7-EAMF, R-J003-S2D4 (D29) — the provider chooser inside `GET /oauth/authorize`



