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

- Phase 01 ⬜ realizes R-39K3-W9HR, R-3AS0-A18G, R-3BZW-NSZ5, R-8DF1-W89F, R-8IAN-FB87, R-VKB6-SHHV, R-4LKF-FB23, R-O1AD-MRKW, R-O2IA-0JBL — module scaffold, composition root, chassis + registry adoption, manifest/VERSION/AGENTS.md, boot smoke
- Phase 02 ⬜ realizes R-3D7T-1KPU, R-3EFP-FCGJ, R-3FNL-T478, R-3GVI-6VXX, R-3I3E-KNOM, R-3JBA-YFFB, R-3LR3-PYWP, R-NFQ1-NA7N — data model, tokens & blob store
- Phase 03 ⬜ realizes R-3MZ0-3QNE, R-3O6W-HIE3, R-3PES-VA4S, R-3QMP-91VH, R-3RUL-MTM6, R-3T2I-0LCV, R-3UAE-ED3K, R-3VIA-S4U9 — signed upload links: mint + public upload ingress
- Phase 04 ⬜ realizes R-3WQ7-5WKY, R-3XY3-JOBN, R-3Z5Z-XG2C, R-40DW-B7T1, R-41LS-OZJQ, R-42TP-2RAF — download surface: public and private tiers
- Phase 05 ⬜ realizes R-441L-GJ14, R-46HE-82II, R-47PA-LU97, R-48X6-ZLZW, R-4A53-DDQL, R-4BCZ-R5HA — content-plane holder endpoint + import acceptor
- Phase 06 ⬜ realizes R-4CKW-4X7Z, R-4DSS-IOYO, R-4F0O-WGPD, R-4G8L-A8G2, R-4HGH-O06R, R-4IOE-1RXG, R-4JWA-FJO5, R-4L46-TBEU, R-4MC3-735J — MCP tool surface
- Phase 07 ⬜ realizes R-4NJZ-KUW8, R-4PZS-CEDM, R-4R7O-Q64B, R-4SFL-3XV0 — event production wired into every domain mutation
- Phase 08 ⬜ realizes R-4TNH-HPLP, R-4UVD-VHCE, R-4W3A-9933, R-4XB6-N0TS, R-4YJ3-0SKH, R-4ZQZ-EKB6, R-50YV-SC1V, R-526S-63SK — nginx location fragment
- Phase 09 ⬜ realizes R-53EO-JVJ9, R-54MK-XN9Y, R-55UH-BF0N, R-572D-P6RC, R-59I6-GQ8Q, R-5AQ2-UHZF — landing page: sortable, filterable inventory
