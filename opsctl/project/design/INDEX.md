# opsctl — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolving an id is a grep against this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — Restore reconstructs `cache/` owned by the service user — owns `R-WP3M-PO1V`, `R-WQBJ-3FSK`, `R-WRJF-H7J9`
- D2 → `project/design/D02.md` — Stage unpacks into a temp dir on the OPSCTL_ROOT filesystem — owns `R-65MT-7QEK`, `R-66UP-LI59`
- D3 → `project/design/D03.md` — opsctl loads the box env file at startup — owns `R-6AIE-QTDC`, `R-6BQB-4L41`, `R-6CY7-ICUQ`, `R-6FE0-9WC4`
- D4 → `project/design/D04.md` — `opsctl deploy` renders and installs the apex block for the DEFAULT app — owns `R-MSOP-5MDA`, `R-MTWL-JE3Z`, `R-MV4H-X5UO`, `R-MXKA-OPC2`, `R-CNPY-3Z4Y`, `R-MYS7-2H2R`
- D5 → `project/design/D05.md` — `opsctl setup` provisions the DEFAULT app without a locations fragment — owns `R-CIUC-KW66`, `R-CK28-YNWV`, `R-CLA5-CFNK`, `R-CMI1-Q7E9`
- D6 → `project/design/D06.md` — init-box creates the `web` group and makes nginx a member — owns `R-AQMT-9M04`, `R-ARUP-NDQT`
- D7 → `project/design/D07.md` — setup provisions the served `www` tree (`public`/`private`, no `working`) as `<app>:web`, setgid, via one `ensureWWWPerms` helper — owns `R-AUAI-EX87`, `R-QEPF-HJ11`, `R-QFXB-VARQ`
- D8 → `project/design/D08.md` — deploy re-asserts the served-tree `web` invariant after the state-ownership chown — owns `R-AVIE-SOYW`, `R-AWQB-6GPL`, `R-AXY7-K8GA`
- D9 → `project/design/D09.md` — restore re-asserts the served-tree `web` invariant after replacing state — owns `R-AZ63-Y06Z`, `R-B0E0-BRXO`

## Verification ids → Decision

- R-65MT-7QEK → D2 — `project/design/D02.md`
- R-66UP-LI59 → D2 — `project/design/D02.md`
- R-6AIE-QTDC → D3 — `project/design/D03.md`
- R-6BQB-4L41 → D3 — `project/design/D03.md`
- R-6CY7-ICUQ → D3 — `project/design/D03.md`
- R-6FE0-9WC4 → D3 — `project/design/D03.md`
- R-AQMT-9M04 → D6 — `project/design/D06.md`
- R-ARUP-NDQT → D6 — `project/design/D06.md`
- R-AUAI-EX87 → D7 — `project/design/D07.md`
- R-AVIE-SOYW → D8 — `project/design/D08.md`
- R-AWQB-6GPL → D8 — `project/design/D08.md`
- R-AXY7-K8GA → D8 — `project/design/D08.md`
- R-AZ63-Y06Z → D9 — `project/design/D09.md`
- R-B0E0-BRXO → D9 — `project/design/D09.md`
- R-CIUC-KW66 → D5 — `project/design/D05.md`
- R-CK28-YNWV → D5 — `project/design/D05.md`
- R-CLA5-CFNK → D5 — `project/design/D05.md`
- R-CMI1-Q7E9 → D5 — `project/design/D05.md`
- R-CNPY-3Z4Y → D4 — `project/design/D04.md`
- R-MSOP-5MDA → D4 — `project/design/D04.md`
- R-MTWL-JE3Z → D4 — `project/design/D04.md`
- R-MV4H-X5UO → D4 — `project/design/D04.md`
- R-MXKA-OPC2 → D4 — `project/design/D04.md`
- R-MYS7-2H2R → D4 — `project/design/D04.md`
- R-QEPF-HJ11 → D7 — `project/design/D07.md`
- R-QFXB-VARQ → D7 — `project/design/D07.md`
- R-WP3M-PO1V → D1 — `project/design/D01.md`
- R-WQBJ-3FSK → D1 — `project/design/D01.md`
- R-WRJF-H7J9 → D1 — `project/design/D01.md`
