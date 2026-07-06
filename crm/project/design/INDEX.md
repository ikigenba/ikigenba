# crm — Design Index

Each Decision maps to its `project/design/DNN.md`; every `R-XXXX-XXXX` id maps to its Decision/file. Resolve an id by grepping this index (or the Decision files directly). Regenerate this manifest whenever a Decision is added or its Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — The landing handler and its v1 content (service name + version) — owns R-LAND-2K7P, R-LAND-4M9Q, R-LAND-6N3R, R-LAND-8P5S
- D2 → `project/design/D02.md` — Route wiring: `GET /{$}` mounted ungated through `Spec.Handlers` — owns R-ROUT-3T2V, R-ROUT-5W4X, R-ROUT-7Y6Z
- D3 → `project/design/D03.md` — crm's own Carbon design assets (shipped in `share/www/static`) — owns R-ASST-2B8C, R-ASST-4D1E, R-ASST-6F3G
- D4 → `project/design/D04.md` — nginx fragment: the exact-match session-gated `= /srv/crm/` location — owns R-NGNX-2H5J, R-NGNX-4K7L, R-NGNX-6M9N, R-NGNX-8P1Q
- D5 → `project/design/D05.md` — Docs state current truth: purge the stale "no UI" line — none (structural; docs-only)
- D6 → `project/design/D06.md` — Conform the landing page to the cron canonical template — none (structural; markup-only)
- D7 → `project/design/D07.md` — A top-left Home link to the dashboard landing page — owns R-HOME-3L5Q
- D8 → `project/design/D08.md` — Self-serve the landing page's fonts and eliminate the FOUT (relative stylesheet link + `font-display: optional` + self-served `src` + `<head>` preload + session-gated nginx `/srv/crm/static/`) — owns R-SRS9-B2RI, R-ST05-OUI7, R-SU82-2M8W, R-SVFY-GDZL, R-SWNU-U5QA
- D9 → `project/design/D09.md` — Self-routing service `instructions` (Tier 0: vocabulary + verb-flow + guide pointer) — owns R-PDZ7-HTAN, R-PF73-VL1C
- D10 → `project/design/D10.md` — Lean tool descriptions: relocate the `save` field catalog out of the always-loaded listing — owns R-PGF0-9CS1, R-PIUT-0W9F
- D11 → `project/design/D11.md` — The `guide` tool and its embedded document (Tier 2: on-demand field catalogs + worked examples) — owns R-PK2P-EO04, R-PLAL-SFQT, R-PMII-67HI
- D12 → `project/design/D12.md` — Web surface from `share/www` through the chassis (de-embed; `Spec.WWW`, delete `internal/web`) — owns R-MTM5-0PXH, R-MUU1-EHO6
- D13 → `project/design/D13.md` — MCP surface over `appkit/mcp`: `internal/mcp` becomes the tool table — owns R-MW1X-S9EV
- D14 → `project/design/D14.md` — Delete the chassis shims: `internal/ids` and the `internal/db` wrappers — none (structural)

## Verification ids → Decision

- R-ASST-2B8C → D3 → `project/design/D03.md`
- R-ASST-4D1E → D3 → `project/design/D03.md`
- R-ASST-6F3G → D3 → `project/design/D03.md`
- R-HOME-3L5Q → D7 → `project/design/D07.md`
- R-LAND-2K7P → D1 → `project/design/D01.md`
- R-LAND-4M9Q → D1 → `project/design/D01.md`
- R-LAND-6N3R → D1 → `project/design/D01.md`
- R-LAND-8P5S → D1 → `project/design/D01.md`
- R-MTM5-0PXH → D12 → `project/design/D12.md`
- R-MUU1-EHO6 → D12 → `project/design/D12.md`
- R-MW1X-S9EV → D13 → `project/design/D13.md`
- R-NGNX-2H5J → D4 → `project/design/D04.md`
- R-NGNX-4K7L → D4 → `project/design/D04.md`
- R-NGNX-6M9N → D4 → `project/design/D04.md`
- R-NGNX-8P1Q → D4 → `project/design/D04.md`
- R-PDZ7-HTAN → D9 → `project/design/D09.md`
- R-PF73-VL1C → D9 → `project/design/D09.md`
- R-PGF0-9CS1 → D10 → `project/design/D10.md`
- R-PIUT-0W9F → D10 → `project/design/D10.md`
- R-PK2P-EO04 → D11 → `project/design/D11.md`
- R-PLAL-SFQT → D11 → `project/design/D11.md`
- R-PMII-67HI → D11 → `project/design/D11.md`
- R-ROUT-3T2V → D2 → `project/design/D02.md`
- R-ROUT-5W4X → D2 → `project/design/D02.md`
- R-ROUT-7Y6Z → D2 → `project/design/D02.md`
- R-SRS9-B2RI → D8 → `project/design/D08.md`
- R-ST05-OUI7 → D8 → `project/design/D08.md`
- R-SU82-2M8W → D8 → `project/design/D08.md`
- R-SVFY-GDZL → D8 → `project/design/D08.md`
- R-SWNU-U5QA → D8 → `project/design/D08.md`
