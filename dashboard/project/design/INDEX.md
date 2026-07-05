# dashboard — Design Index (web pages restructure)

Each Decision maps to its `project/design/DNN.md`; every `R-XXXX-XXXX` id maps to
its Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Three-page topology and the route map — owns R-DB01-PG3A, R-DB02-LND7, R-DB03-PRF9
- D2 → `project/design/D02.md` — The profile route is session-gated in-process (redirect when signed out) — owns R-DB04-GATE, R-DB05-SESS
- D3 → `project/design/D03.md` — Personal-access-token management moves to the profile page — owns R-DB06-PATM, R-DB07-PATR, R-DB08-PATX
- D4 → `project/design/D04.md` — OAuth grant management moves to the profile page — owns R-DB09-GRNT, R-DB10-GRVK, R-DB11-GRNX
- D5 → `project/design/D05.md` — Landing composition: service-name links, profile-avatar nav, sign-out, shared list-chrome service list with copyable URLs — owns R-DB12-LINK, R-DB14-SOUT, R-DB15-INST, R-XO4W-LKAI, R-OF1Q-VEDC, R-OG9N-9641, R-OHHJ-MXUQ
- D6 → `project/design/D06.md` — Purge the stale "single hybrid page / don't split" doc rule — owns R-DB16-DOCS
- D7 → `project/design/D07.md` — Login composition: the sign-in line is the `<h1>`; name-origin colophon below the CTA — owns R-DB17-ORIG, R-DB18-KEEP, R-DB19-LAND, R-O7K1-XEN7
- D8 → `project/design/D08.md` — Eliminate the web-font FOUT (font-display: optional + font preload) — owns R-P97M-GIJ1, R-PAFI-UA9Q, R-PBNF-820F
- D9 → `project/design/D09.md` — No site footer: remove the `.site-footer` element from every page — owns R-EFJZ-FRQ1
- D10 → `project/design/D10.md` — The banner is shared app-shell chrome: wordmark links home, monogram avatar + sign-out on both landing and profile — owns R-VTIE-IUFA, R-VUQA-WM5Z, R-VVY7-ADWO
- D11 → `project/design/D11.md` — Telemetry store: in-memory ring-buffer series + snapshot — owns R-EZVQ-IQOL, R-F13M-WIFA, R-F2BJ-AA5Z
- D12 → `project/design/D12.md` — Metric source readers + service discovery — owns R-F4RC-1TND, R-F5Z8-FLE2, R-F774-TD4R, R-F8F1-74VG, R-F9MX-KWM5, R-FAUT-YOCU, R-FC2Q-CG3J
- D13 → `project/design/D13.md` — Collector worker: startup sample + 60s ticker on the serve context — owns R-FDAM-Q7U8, R-FEIJ-3ZKX, R-FFQF-HRBM, R-FGYB-VJ2B
- D14 → `project/design/D14.md` — HTTP surface: `/telemetry` page, `/telemetry/fragment`, the 60s poll — owns R-FI68-9AT0, R-FJE4-N2JP, R-FKM1-0UAE, R-FLTX-EM13, R-FN1T-SDRS
- D15 → `project/design/D15.md` — Chart rendering: hero line charts + stacked-area charts — owns R-FO9Q-65IH, R-FPHM-JX96, R-FQPI-XOZV, R-FRXF-BGQK, R-FT5B-P8H9, R-FUD8-307Y, R-FVL4-GRYN
- D16 → `project/design/D16.md` — Landing entry: a tile linking to the telemetry page — owns R-FWT0-UJPC, R-FY0X-8BG1

## Verification ids → Decision

- R-DB01-PG3A → D1 → `project/design/D01.md`
- R-DB02-LND7 → D1 → `project/design/D01.md`
- R-DB03-PRF9 → D1 → `project/design/D01.md`
- R-DB04-GATE → D2 → `project/design/D02.md`
- R-DB05-SESS → D2 → `project/design/D02.md`
- R-DB06-PATM → D3 → `project/design/D03.md`
- R-DB07-PATR → D3 → `project/design/D03.md`
- R-DB08-PATX → D3 → `project/design/D03.md`
- R-DB09-GRNT → D4 → `project/design/D04.md`
- R-DB10-GRVK → D4 → `project/design/D04.md`
- R-DB11-GRNX → D4 → `project/design/D04.md`
- R-DB12-LINK → D5 → `project/design/D05.md`
- R-DB14-SOUT → D5 → `project/design/D05.md`
- R-DB15-INST → D5 → `project/design/D05.md`
- R-DB16-DOCS → D6 → `project/design/D06.md`
- R-DB17-ORIG → D7 → `project/design/D07.md`
- R-DB18-KEEP → D7 → `project/design/D07.md`
- R-DB19-LAND → D7 → `project/design/D07.md`
- R-EFJZ-FRQ1 → D9 → `project/design/D09.md`
- R-EZVQ-IQOL → D11 → `project/design/D11.md`
- R-F13M-WIFA → D11 → `project/design/D11.md`
- R-F2BJ-AA5Z → D11 → `project/design/D11.md`
- R-F4RC-1TND → D12 → `project/design/D12.md`
- R-F5Z8-FLE2 → D12 → `project/design/D12.md`
- R-F774-TD4R → D12 → `project/design/D12.md`
- R-F8F1-74VG → D12 → `project/design/D12.md`
- R-F9MX-KWM5 → D12 → `project/design/D12.md`
- R-FAUT-YOCU → D12 → `project/design/D12.md`
- R-FC2Q-CG3J → D12 → `project/design/D12.md`
- R-FDAM-Q7U8 → D13 → `project/design/D13.md`
- R-FEIJ-3ZKX → D13 → `project/design/D13.md`
- R-FFQF-HRBM → D13 → `project/design/D13.md`
- R-FGYB-VJ2B → D13 → `project/design/D13.md`
- R-FI68-9AT0 → D14 → `project/design/D14.md`
- R-FJE4-N2JP → D14 → `project/design/D14.md`
- R-FKM1-0UAE → D14 → `project/design/D14.md`
- R-FLTX-EM13 → D14 → `project/design/D14.md`
- R-FN1T-SDRS → D14 → `project/design/D14.md`
- R-FO9Q-65IH → D15 → `project/design/D15.md`
- R-FPHM-JX96 → D15 → `project/design/D15.md`
- R-FQPI-XOZV → D15 → `project/design/D15.md`
- R-FRXF-BGQK → D15 → `project/design/D15.md`
- R-FT5B-P8H9 → D15 → `project/design/D15.md`
- R-FUD8-307Y → D15 → `project/design/D15.md`
- R-FVL4-GRYN → D15 → `project/design/D15.md`
- R-FWT0-UJPC → D16 → `project/design/D16.md`
- R-FY0X-8BG1 → D16 → `project/design/D16.md`
- R-O7K1-XEN7 → D7 → `project/design/D07.md`
- R-OF1Q-VEDC → D5 → `project/design/D05.md`
- R-OG9N-9641 → D5 → `project/design/D05.md`
- R-OHHJ-MXUQ → D5 → `project/design/D05.md`
- R-P97M-GIJ1 → D8 → `project/design/D08.md`
- R-PAFI-UA9Q → D8 → `project/design/D08.md`
- R-PBNF-820F → D8 → `project/design/D08.md`
- R-VTIE-IUFA → D10 → `project/design/D10.md`
- R-VUQA-WM5Z → D10 → `project/design/D10.md`
- R-VVY7-ADWO → D10 → `project/design/D10.md`
- R-XO4W-LKAI → D5 → `project/design/D05.md`
