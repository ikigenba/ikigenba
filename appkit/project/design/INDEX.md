# appkit — Design Index

Each Decision maps to its `project/design/DNN.md`; every `R-XXXX-XXXX` id maps to
its Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Manifest readers resolve *through* the per-app `current` symlink (`appkit/inventory`) — owns R-YO06-9I18, R-YP82-N9RX
- D2 → `project/design/D02.md` — `bin/registry` resolves through `current` — owns R-YQFZ-11IM
- D3 → `project/design/D03.md` — Local dev runtime layout mirrors the box (`bin/start` stages a prod-shaped manifest root) — owns R-YRNV-ET9B
- D4 → `project/design/D04.md` — Retire the stable sibling path and its hand-placed artifacts — owns R-YSVR-SL00, R-YU3O-6CQP
- D5 → `project/design/D05.md` — WWW-root resolution in `appkit/config` (`share/current/www` on box, `./share/www` dev, `<APP>_WWW_PATH` override) — owns R-LWOU-OWWQ, R-LXWR-2ONF, R-LZ4N-GGE4
- D6 → `project/design/D06.md` — The `appkit/web` package: templates + static assets over an on-disk root — owns R-M0CJ-U84T, R-M1KG-7ZVI, R-M2SC-LRM7, R-M408-ZJCW, R-M585-DB3L
- D7 → `project/design/D07.md` — Chassis integration: `Spec.WWW`, the auto-mounted static route, `Router.WWW()` — owns R-M7NY-4UKZ, R-M8VU-IMBO, R-MA3Q-WE2D, R-MBBN-A5T2
- D8 → `project/design/D08.md` — The `appkit/mcp` JSON-RPC transport over a declared tool table — owns R-MCJJ-NXJR, R-MDRG-1PAG, R-MEZC-FH15, R-MG78-T8RU, R-MHF5-70IJ, R-MIN1-KS98, R-MJUX-YJZX
- D9 → `project/design/D09.md` — Chassis-owned standard tools: `health` and `reflection` — owns R-ML2U-CBQM, R-MMAQ-Q3HB, R-MNIN-3V80, R-MOQJ-HMYP

## Verification ids → Decision

- R-LWOU-OWWQ → D5 → `project/design/D05.md`
- R-LXWR-2ONF → D5 → `project/design/D05.md`
- R-LZ4N-GGE4 → D5 → `project/design/D05.md`
- R-M0CJ-U84T → D6 → `project/design/D06.md`
- R-M1KG-7ZVI → D6 → `project/design/D06.md`
- R-M2SC-LRM7 → D6 → `project/design/D06.md`
- R-M408-ZJCW → D6 → `project/design/D06.md`
- R-M585-DB3L → D6 → `project/design/D06.md`
- R-M7NY-4UKZ → D7 → `project/design/D07.md`
- R-M8VU-IMBO → D7 → `project/design/D07.md`
- R-MA3Q-WE2D → D7 → `project/design/D07.md`
- R-MBBN-A5T2 → D7 → `project/design/D07.md`
- R-MCJJ-NXJR → D8 → `project/design/D08.md`
- R-MDRG-1PAG → D8 → `project/design/D08.md`
- R-MEZC-FH15 → D8 → `project/design/D08.md`
- R-MG78-T8RU → D8 → `project/design/D08.md`
- R-MHF5-70IJ → D8 → `project/design/D08.md`
- R-MIN1-KS98 → D8 → `project/design/D08.md`
- R-MJUX-YJZX → D8 → `project/design/D08.md`
- R-ML2U-CBQM → D9 → `project/design/D09.md`
- R-MMAQ-Q3HB → D9 → `project/design/D09.md`
- R-MNIN-3V80 → D9 → `project/design/D09.md`
- R-MOQJ-HMYP → D9 → `project/design/D09.md`
- R-YO06-9I18 → D1 → `project/design/D01.md`
- R-YP82-N9RX → D1 → `project/design/D01.md`
- R-YQFZ-11IM → D2 → `project/design/D02.md`
- R-YRNV-ET9B → D3 → `project/design/D03.md`
- R-YSVR-SL00 → D4 → `project/design/D04.md`
- R-YU3O-6CQP → D4 → `project/design/D04.md`
