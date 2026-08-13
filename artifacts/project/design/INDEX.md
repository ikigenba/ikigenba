# artifacts — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. To resolve an id, grep this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Service skeleton, chassis adoption & composition root — owns R-39K3-W9HR, R-3AS0-A18G, R-3BZW-NSZ5; adopts R-8DF1-W89F, R-8IAN-FB87, R-VKB6-SHHV (root `project/design/D11.md`), R-4LKF-FB23 (root `project/design/D08.md`)
- D2 → `project/design/D02.md` — Data model, tokens & blob store — owns R-3D7T-1KPU, R-3EFP-FCGJ, R-3FNL-T478, R-3GVI-6VXX, R-3I3E-KNOM, R-3JBA-YFFB, R-3LR3-PYWP; adopts R-NFQ1-NA7N (root `project/design/D25.md`)
- D3 → `project/design/D03.md` — Signed upload links: mint + the public upload ingress — owns R-3MZ0-3QNE, R-3O6W-HIE3, R-3PES-VA4S, R-3QMP-91VH, R-3RUL-MTM6, R-3T2I-0LCV, R-3UAE-ED3K, R-3VIA-S4U9, R-78BZ-IXSS, R-79JV-WPJH, R-7BZO-O90V
- D4 → `project/design/D04.md` — The download surface: public and private tiers — owns R-3WQ7-5WKY, R-3XY3-JOBN, R-3Z5Z-XG2C, R-40DW-B7T1, R-41LS-OZJQ, R-42TP-2RAF
- D5 → `project/design/D05.md` — Content-plane citizenship: holder endpoint + `import` acceptor — owns R-441L-GJ14, R-46HE-82II, R-47PA-LU97, R-48X6-ZLZW, R-4A53-DDQL, R-4BCZ-R5HA
- D6 → `project/design/D06.md` — MCP tool surface — owns R-4CKW-4X7Z, R-4DSS-IOYO, R-4F0O-WGPD, R-4G8L-A8G2, R-4HGH-O06R, R-4IOE-1RXG, R-4JWA-FJO5, R-4L46-TBEU, R-4MC3-735J, R-7D7L-20RK, R-P52Q-H8MG
- D7 → `project/design/D07.md` — Event production: `created` / `updated` / `deleted` — owns R-4NJZ-KUW8, R-4PZS-CEDM, R-4R7O-Q64B, R-4SFL-3XV0
- D8 → `project/design/D08.md` — nginx location fragment (tiers) — owns R-4TNH-HPLP, R-4UVD-VHCE, R-4W3A-9933, R-4XB6-N0TS, R-4YJ3-0SKH, R-4ZQZ-EKB6, R-50YV-SC1V, R-526S-63SK
- D9 → `project/design/D09.md` — The landing page: a sortable, filterable inventory — owns R-53EO-JVJ9, R-54MK-XN9Y, R-55UH-BF0N, R-572D-P6RC, R-5AQ2-UHZF, R-OSZJ-I2D8, R-OU7F-VU3X
- D10 → `project/design/D10.md` — Test strategy: adopt the suite testing-language contract — mints none; adopts R-O1AD-MRKW, R-O2IA-0JBL (root `project/design/D23.md`)
- D11 → `project/design/D11.md` — Adopt the suite brand icon contract: the shipped icon set and its link markup — mints none; adopts R-8MFA-HUNC, R-RYDN-YNR5, R-RZLK-CFHU (root `project/design/D29.md`)

## Verification ids → Decision

- R-39K3-W9HR → D1 — `project/design/D01.md`
- R-3AS0-A18G → D1 — `project/design/D01.md`
- R-3BZW-NSZ5 → D1 — `project/design/D01.md`
- R-3D7T-1KPU → D2 — `project/design/D02.md`
- R-3EFP-FCGJ → D2 — `project/design/D02.md`
- R-3FNL-T478 → D2 — `project/design/D02.md`
- R-3GVI-6VXX → D2 — `project/design/D02.md`
- R-3I3E-KNOM → D2 — `project/design/D02.md`
- R-3JBA-YFFB → D2 — `project/design/D02.md`
- R-3LR3-PYWP → D2 — `project/design/D02.md`
- R-3MZ0-3QNE → D3 — `project/design/D03.md`
- R-3O6W-HIE3 → D3 — `project/design/D03.md`
- R-3PES-VA4S → D3 — `project/design/D03.md`
- R-3QMP-91VH → D3 — `project/design/D03.md`
- R-3RUL-MTM6 → D3 — `project/design/D03.md`
- R-3T2I-0LCV → D3 — `project/design/D03.md`
- R-3UAE-ED3K → D3 — `project/design/D03.md`
- R-3VIA-S4U9 → D3 — `project/design/D03.md`
- R-3WQ7-5WKY → D4 — `project/design/D04.md`
- R-3XY3-JOBN → D4 — `project/design/D04.md`
- R-3Z5Z-XG2C → D4 — `project/design/D04.md`
- R-40DW-B7T1 → D4 — `project/design/D04.md`
- R-41LS-OZJQ → D4 — `project/design/D04.md`
- R-42TP-2RAF → D4 — `project/design/D04.md`
- R-441L-GJ14 → D5 — `project/design/D05.md`
- R-46HE-82II → D5 — `project/design/D05.md`
- R-47PA-LU97 → D5 — `project/design/D05.md`
- R-48X6-ZLZW → D5 — `project/design/D05.md`
- R-4A53-DDQL → D5 — `project/design/D05.md`
- R-4BCZ-R5HA → D5 — `project/design/D05.md`
- R-4CKW-4X7Z → D6 — `project/design/D06.md`
- R-4DSS-IOYO → D6 — `project/design/D06.md`
- R-4F0O-WGPD → D6 — `project/design/D06.md`
- R-4G8L-A8G2 → D6 — `project/design/D06.md`
- R-4HGH-O06R → D6 — `project/design/D06.md`
- R-4IOE-1RXG → D6 — `project/design/D06.md`
- R-4JWA-FJO5 → D6 — `project/design/D06.md`
- R-4L46-TBEU → D6 — `project/design/D06.md`
- R-4LKF-FB23 → D1 — `project/design/D01.md` (adopted from root `project/design/D08.md`)
- R-4MC3-735J → D6 — `project/design/D06.md`
- R-4NJZ-KUW8 → D7 — `project/design/D07.md`
- R-4PZS-CEDM → D7 — `project/design/D07.md`
- R-4R7O-Q64B → D7 — `project/design/D07.md`
- R-4SFL-3XV0 → D7 — `project/design/D07.md`
- R-4TNH-HPLP → D8 — `project/design/D08.md`
- R-4UVD-VHCE → D8 — `project/design/D08.md`
- R-4W3A-9933 → D8 — `project/design/D08.md`
- R-4XB6-N0TS → D8 — `project/design/D08.md`
- R-4YJ3-0SKH → D8 — `project/design/D08.md`
- R-4ZQZ-EKB6 → D8 — `project/design/D08.md`
- R-50YV-SC1V → D8 — `project/design/D08.md`
- R-526S-63SK → D8 — `project/design/D08.md`
- R-53EO-JVJ9 → D9 — `project/design/D09.md`
- R-54MK-XN9Y → D9 — `project/design/D09.md`
- R-55UH-BF0N → D9 — `project/design/D09.md`
- R-572D-P6RC → D9 — `project/design/D09.md`
- R-5AQ2-UHZF → D9 — `project/design/D09.md`
- R-78BZ-IXSS → D3 — `project/design/D03.md`
- R-79JV-WPJH → D3 — `project/design/D03.md`
- R-7BZO-O90V → D3 — `project/design/D03.md`
- R-7D7L-20RK → D6 — `project/design/D06.md`
- R-8DF1-W89F → D1 — `project/design/D01.md` (adopted from root `project/design/D11.md`)
- R-8IAN-FB87 → D1 — `project/design/D01.md` (adopted from root `project/design/D11.md`)
- R-8MFA-HUNC → D11 — `project/design/D11.md` (adopted from root `project/design/D29.md`)
- R-NFQ1-NA7N → D2 — `project/design/D02.md` (adopted from root `project/design/D25.md`)
- R-O1AD-MRKW → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-O2IA-0JBL → D10 — `project/design/D10.md` (adopted from root `project/design/D23.md`)
- R-OSZJ-I2D8 → D9 — `project/design/D09.md`
- R-OU7F-VU3X → D9 — `project/design/D09.md`
- R-P52Q-H8MG → D6 — `project/design/D06.md`
- R-RYDN-YNR5 → D11 — `project/design/D11.md` (adopted from root `project/design/D29.md`)
- R-RZLK-CFHU → D11 — `project/design/D11.md` (adopted from root `project/design/D29.md`)
- R-VKB6-SHHV → D1 — `project/design/D01.md` (adopted from root `project/design/D11.md`)

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests prove it; the quality of each proof is the audit's
question, the mapping's completeness is this manifest's. Regenerated with the
rest of the index.

1. Upload URL + `curl` via MCP; file uploads and appears in the list (URLs
   usable from any machine, on the configured public base) →
   R-P52Q-H8MG, R-4CKW-4X7Z, R-3O6W-HIE3, R-4F0O-WGPD, R-78BZ-IXSS,
   R-79JV-WPJH, R-7D7L-20RK
2. Upload link single-use, 24h expiry, failed attempt leaves it usable →
   R-3PES-VA4S, R-3QMP-91VH, R-3UAE-ED3K
3. Over-long filename rejected intact — nothing stored, nothing truncated →
   R-3FNL-T478, R-3MZ0-3QNE
4. Upload over the configured cap refused, no file appears → R-3QMP-91VH
5. Public link downloads exact bytes + filename with no credential →
   R-3WQ7-5WKY
6. Private link serves logged-in users; logged-out browser lands on sign-in →
   R-3XY3-JOBN, R-50YV-SC1V
7. Links unguessable; a made-up link refused revealing nothing →
   R-3EFP-FCGJ, R-3Z5Z-XG2C
8. Public→private flip cuts anonymous download; flipping back restores it →
   R-41LS-OZJQ, R-4IOE-1RXG
9. Import from dropbox by reference, byte-identical, no content in any MCP
   result → R-48X6-ZLZW, R-4DSS-IOYO, R-4F0O-WGPD
10. Another service fetches a stored file by reference, byte-identical →
    R-441L-GJ14, R-47PA-LU97
11. Delete kills the link and removes the entry → R-4JWA-FJO5
12. Each download raises the count by one → R-42TP-2RAF
13. Landing page sorts, filters, downloads; sessionless browser refused →
    R-53EO-JVJ9, R-55UH-BF0N, R-OU7F-VU3X, R-4XB6-N0TS
14. Created/updated/deleted events observable by a consumer →
    R-4NJZ-KUW8, R-4R7O-Q64B, R-4SFL-3XV0
