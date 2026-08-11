# artifacts — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and
the **only** place a phase's pending marker lives. Each phase line is a
Markdown bullet beginning with `- Phase` followed by its zero-padded number,
the marker `⬜` (pending), then `realizes <Decision ids>` (or `realizes —`
for a purely structural phase), then `— <one cohesive objective>`. The build
loop finds its next unit of work with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`, reads only that
phase's `project/plan/phase-NN.md`, and on completion **deletes** that
phase's line here together with its `phase-NN.md` — there is no done marker;
done is gone. A phase body file carries no marker of its own. This document
deliberately carries **no** bare status glyph outside the phase lines, so
the anchored grep matches only phase lines.

Next phase: 10

- Phase 06 ⬜ realizes R-4CKW-4X7Z, R-4DSS-IOYO, R-4F0O-WGPD, R-4G8L-A8G2, R-4HGH-O06R, R-4IOE-1RXG, R-4JWA-FJO5, R-4L46-TBEU, R-4MC3-735J — MCP tool surface
- Phase 07 ⬜ realizes R-4NJZ-KUW8, R-4PZS-CEDM, R-4R7O-Q64B, R-4SFL-3XV0 — event production wired into every domain mutation
- Phase 08 ⬜ realizes R-4TNH-HPLP, R-4UVD-VHCE, R-4W3A-9933, R-4XB6-N0TS, R-4YJ3-0SKH, R-4ZQZ-EKB6, R-50YV-SC1V, R-526S-63SK — nginx location fragment
- Phase 09 ⬜ realizes R-53EO-JVJ9, R-54MK-XN9Y, R-55UH-BF0N, R-572D-P6RC, R-59I6-GQ8Q, R-5AQ2-UHZF — landing page: sortable, filterable inventory
