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
- D7 → `project/design/D07.md` — setup provisions the served `www` tree (`public`/`private`, no `working`) owned by the service user — owns `R-QFXB-VARQ`, `R-3K9X-IPJZ`, `R-AUAI-EX87`, `R-3LHT-WHAO`
- D8 → `project/design/D08.md` — deploy's state-ownership chown already owns the served tree; no separate www step — owns `R-3MPQ-A91D`, `R-AXY7-K8GA`
- D9 → `project/design/D09.md` — restore reconstitutes the served tree's ownership to the service user — owns `R-3NXM-O0S2`, `R-B0E0-BRXO`
- D10 → `project/design/D10.md` — init-box installs the box-baseline command-line tooling (poppler-utils, git, sqlite, tar, curl-minimal) — owns `R-JQGB-RYA2`, `R-JRO8-5Q0R`
- D11 → `project/design/D11.md` — init-box installs the oauth CLI via its release installer to `/usr/local/bin` — owns `R-ML75-3NVZ`, `R-MMF1-HFMO`
- D12 → `project/design/D12.md` — `cache/` is app-owned by construction: setup creates it owned by the service user; deploy re-asserts it alongside `state/` — owns `R-4ZI0-4CH5`, `R-50PW-I47U`
- D13 → `project/design/D13.md` — opsctl-owned `backup` / `restore`, S3-only (stop·snapshot·start) — owns `R-4GOT-W83B`, `R-4HWQ-9ZU0`, `R-4J4M-NRKP`, `R-4KCJ-1JBE`, `R-82FY-GAL6`, `R-TAOX-5LKS`, `R-TBWT-JDBH`
- D14 → `project/design/D14.md` — Scheduled nightly backup (systemd timer + box sweep) — owns `R-RNKC-HAW8`, `R-ROS8-V2MX`
- D15 → `project/design/D15.md` — stage / deploy / rollback / prune orchestration — owns `R-84VR-7U2K`, `R-863N-LLT9`, `R-87BJ-ZDJY`, `R-88JG-D5AN`, `R-89RC-QX1C`, `R-8AZ9-4OS1`, `R-8C75-IGIQ`, `R-I80H-SAQ3`
- D16 → `project/design/D16.md` — Stage preflight without the retired manifest verb; one version channel — owns `R-TA75-P0NF`, `R-TBF2-2SE4`
- D17 → `project/design/D17.md` — The testing-language contract: opsctl is hermetic + manual, and its out-of-loop ids are the manual layer — owns `R-2B4O-Z98N`; cites `R-O1AD-MRKW`, `R-O2IA-0JBL` (owned by `root project/design/D23.md`, `[proof: per-service]`)
- D18 → `project/design/D18.md` — Adopt the suite lint contract (`root project/design/D30.md`) at tier `cheap` — none (structural; the contract carries no per-service ids)

## Verification ids → Decision

- R-2B4O-Z98N → D17 — `project/design/D17.md`
- R-3K9X-IPJZ → D7 — `project/design/D07.md`
- R-3LHT-WHAO → D7 — `project/design/D07.md`
- R-3MPQ-A91D → D8 — `project/design/D08.md`
- R-3NXM-O0S2 → D9 — `project/design/D09.md`
- R-4GOT-W83B → D13 — `project/design/D13.md`
- R-4HWQ-9ZU0 → D13 — `project/design/D13.md`
- R-4J4M-NRKP → D13 — `project/design/D13.md`
- R-4KCJ-1JBE → D13 — `project/design/D13.md`
- R-4ZI0-4CH5 → D12 — `project/design/D12.md`
- R-50PW-I47U → D12 — `project/design/D12.md`
- R-65MT-7QEK → D2 — `project/design/D02.md`
- R-66UP-LI59 → D2 — `project/design/D02.md`
- R-6AIE-QTDC → D3 — `project/design/D03.md`
- R-6BQB-4L41 → D3 — `project/design/D03.md`
- R-6CY7-ICUQ → D3 — `project/design/D03.md`
- R-6FE0-9WC4 → D3 — `project/design/D03.md`
- R-82FY-GAL6 → D13 — `project/design/D13.md`
- R-84VR-7U2K → D15 — `project/design/D15.md`
- R-863N-LLT9 → D15 — `project/design/D15.md`
- R-87BJ-ZDJY → D15 — `project/design/D15.md`
- R-88JG-D5AN → D15 — `project/design/D15.md`
- R-89RC-QX1C → D15 — `project/design/D15.md`
- R-8AZ9-4OS1 → D15 — `project/design/D15.md`
- R-8C75-IGIQ → D15 — `project/design/D15.md`
- R-AUAI-EX87 → D7 — `project/design/D07.md`
- R-AXY7-K8GA → D8 — `project/design/D08.md`
- R-B0E0-BRXO → D9 — `project/design/D09.md`
- R-CIUC-KW66 → D5 — `project/design/D05.md`
- R-CK28-YNWV → D5 — `project/design/D05.md`
- R-CLA5-CFNK → D5 — `project/design/D05.md`
- R-CMI1-Q7E9 → D5 — `project/design/D05.md`
- R-CNPY-3Z4Y → D4 — `project/design/D04.md`
- R-I80H-SAQ3 → D15 — `project/design/D15.md`
- R-JQGB-RYA2 → D10 — `project/design/D10.md`
- R-JRO8-5Q0R → D10 — `project/design/D10.md`
- R-ML75-3NVZ → D11 — `project/design/D11.md`
- R-MMF1-HFMO → D11 — `project/design/D11.md`
- R-MSOP-5MDA → D4 — `project/design/D04.md`
- R-MTWL-JE3Z → D4 — `project/design/D04.md`
- R-MV4H-X5UO → D4 — `project/design/D04.md`
- R-MXKA-OPC2 → D4 — `project/design/D04.md`
- R-MYS7-2H2R → D4 — `project/design/D04.md`
- R-O1AD-MRKW → D17 — `project/design/D17.md` (owned by `root project/design/D23.md`)
- R-O2IA-0JBL → D17 — `project/design/D17.md` (owned by `root project/design/D23.md`)
- R-QFXB-VARQ → D7 — `project/design/D07.md`
- R-RNKC-HAW8 → D14 — `project/design/D14.md`
- R-ROS8-V2MX → D14 — `project/design/D14.md`
- R-TA75-P0NF → D16 — `project/design/D16.md`
- R-TAOX-5LKS → D13 — `project/design/D13.md`
- R-TBF2-2SE4 → D16 — `project/design/D16.md`
- R-TBWT-JDBH → D13 — `project/design/D13.md`
- R-WP3M-PO1V → D1 — `project/design/D01.md`
- R-WQBJ-3FSK → D1 — `project/design/D01.md`
- R-WRJF-H7J9 → D1 — `project/design/D01.md`

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped to
the id(s) whose Verification most directly proves it; the quality of each proof
is the audit's question, the mapping's completeness is this manifest's.
Regenerated with the rest of the index.

1. Cross-device `opsctl stage <app> <version>` completes, release staged, no cross-device failure →
   R-65MT-7QEK, R-66UP-LI59
2. `sudo opsctl <verb>` needing the box env finds it already loaded, no manual load →
   R-6AIE-QTDC, R-6FE0-9WC4
3. Box env genuinely absent — the affected verb fails naming the missing value →
   R-MTWL-JE3Z, R-CNPY-3Z4Y
4. Deploying the apex/`DEFAULT` app updates front-door routing, effective only after it validates; a bad or value-missing routing aborts untouched with a naming message →
   R-MSOP-5MDA, R-MV4H-X5UO, R-MTWL-JE3Z, R-MXKA-OPC2, R-CNPY-3Z4Y
5. A served-file page reachable before a box op stays reachable after — provisioning grants front-door read; deploy/restore leaves it intact →
   R-3K9X-IPJZ, R-3MPQ-A91D, R-3NXM-O0S2
6. After fresh-box provisioning the shared CLI tooling works (PDF→text, `git` clone, `sqlite3`); re-running provisioning still works →
   R-JQGB-RYA2, R-JRO8-5Q0R
7. After fresh-box provisioning the shared OAuth login CLI is installed for any service user and reports its version; re-running reinstalls and it still works →
   R-ML75-3NVZ, R-MMF1-HFMO
