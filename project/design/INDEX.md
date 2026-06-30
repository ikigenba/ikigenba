# Suite on-box layout, versioning & backup/restore — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this file whenever a Decision is added or its Verification
ids change.

## Decisions

- **D1** → `project/design/D01.md` — The `/opt/<svc>/` install tree — ids: R-3SAU-8T9F, R-LHY1-6IS8, R-VB77-BU5O, R-VCF3-PLWD
- **D2** → `project/design/D02.md` — Versioned release bundle: `libexec/` binary + `etc/<v>`/`share/<v>` + symlink swap — ids: R-3TIQ-ML04, R-3UQN-0CQT, R-3VYJ-E4HI, R-1A79-JG03, R-1BF5-X7QS, R-1CN2-AZHH
- **D3** → `project/design/D03.md` — SemVer 2.0 version identity & ordering — ids: R-3X6F-RW87, R-3YEC-5NYW, R-40U4-X7GA, R-4221-AZ6Z, R-439X-OQXO
- **D4** → `project/design/D04.md` — Version production: `bump`/`ship` emit `v`-prefixed SemVer + the release bundle — ids: R-44HU-2IOD, R-45PQ-GAF2, R-P4CO-FY2L
- **D5** → `project/design/D05.md` — The `state/` ÷ `cache/` backup boundary — ids: R-46XM-U25R, R-485J-7TWG, R-49DF-LLN5, R-4ALB-ZDDU
- **D6** → `project/design/D06.md` — Epoch re-mint by exclusion + boot-reconstruction invariant — ids: R-4BT8-D54J, R-4D14-QWV8, R-4E91-4OLX
- **D7** → `project/design/D07.md` — opsctl-owned `backup`/`restore`, S3-only (stop·snapshot·start) — ids: R-4GOT-W83B, R-4HWQ-9ZU0, R-4J4M-NRKP, R-4KCJ-1JBE, R-QQNU-T5M7, R-82FY-GAL6, R-TAOX-5LKS, R-TBWT-JDBH
- **D8** → `project/design/D08.md` — Per-service adoption & live-box migration — ids: R-4LKF-FB23, R-4MSB-T2SS
- **D9** → `project/design/D09.md` — Scheduled nightly backup (systemd timer + box sweep) — ids: R-RNKC-HAW8, R-ROS8-V2MX
- **D10** → `project/design/D10.md` — stage / deploy / rollback / prune orchestration — ids: R-84VR-7U2K, R-863N-LLT9, R-87BJ-ZDJY, R-88JG-D5AN, R-89RC-QX1C, R-8AZ9-4OS1, R-8C75-IGIQ
- **D11** → `project/design/D11.md` — The env contract: portable authored `manifest.env` + `IKIGENBA_ROOT` path composition + reduced verb set — ids: R-8DF1-W89F, R-8EMY-A004, R-8FUU-NRQT, R-8H2R-1JHI, R-8IAN-FB87

## Verification ids → Decision

- R-1A79-JG03 → D2 (`project/design/D02.md`)
- R-1BF5-X7QS → D2 (`project/design/D02.md`)
- R-1CN2-AZHH → D2 (`project/design/D02.md`)
- R-3SAU-8T9F → D1 (`project/design/D01.md`)
- R-3TIQ-ML04 → D2 (`project/design/D02.md`)
- R-3UQN-0CQT → D2 (`project/design/D02.md`)
- R-3VYJ-E4HI → D2 (`project/design/D02.md`)
- R-3X6F-RW87 → D3 (`project/design/D03.md`)
- R-3YEC-5NYW → D3 (`project/design/D03.md`)
- R-40U4-X7GA → D3 (`project/design/D03.md`)
- R-4221-AZ6Z → D3 (`project/design/D03.md`)
- R-439X-OQXO → D3 (`project/design/D03.md`)
- R-44HU-2IOD → D4 (`project/design/D04.md`)
- R-45PQ-GAF2 → D4 (`project/design/D04.md`)
- R-46XM-U25R → D5 (`project/design/D05.md`)
- R-485J-7TWG → D5 (`project/design/D05.md`)
- R-49DF-LLN5 → D5 (`project/design/D05.md`)
- R-4ALB-ZDDU → D5 (`project/design/D05.md`)
- R-4BT8-D54J → D6 (`project/design/D06.md`)
- R-4D14-QWV8 → D6 (`project/design/D06.md`)
- R-4E91-4OLX → D6 (`project/design/D06.md`)
- R-4GOT-W83B → D7 (`project/design/D07.md`)
- R-4HWQ-9ZU0 → D7 (`project/design/D07.md`)
- R-4J4M-NRKP → D7 (`project/design/D07.md`)
- R-4KCJ-1JBE → D7 (`project/design/D07.md`)
- R-4LKF-FB23 → D8 (`project/design/D08.md`)
- R-4MSB-T2SS → D8 (`project/design/D08.md`)
- R-82FY-GAL6 → D7 (`project/design/D07.md`)
- R-84VR-7U2K → D10 (`project/design/D10.md`)
- R-863N-LLT9 → D10 (`project/design/D10.md`)
- R-87BJ-ZDJY → D10 (`project/design/D10.md`)
- R-88JG-D5AN → D10 (`project/design/D10.md`)
- R-89RC-QX1C → D10 (`project/design/D10.md`)
- R-8AZ9-4OS1 → D10 (`project/design/D10.md`)
- R-8C75-IGIQ → D10 (`project/design/D10.md`)
- R-8DF1-W89F → D11 (`project/design/D11.md`)
- R-8EMY-A004 → D11 (`project/design/D11.md`)
- R-8FUU-NRQT → D11 (`project/design/D11.md`)
- R-8H2R-1JHI → D11 (`project/design/D11.md`)
- R-8IAN-FB87 → D11 (`project/design/D11.md`)
- R-LHY1-6IS8 → D1 (`project/design/D01.md`)
- R-P4CO-FY2L → D4 (`project/design/D04.md`)
- R-QQNU-T5M7 → D7 (`project/design/D07.md`)
- R-RNKC-HAW8 → D9 (`project/design/D09.md`)
- R-ROS8-V2MX → D9 (`project/design/D09.md`)
- R-TAOX-5LKS → D7 (`project/design/D07.md`)
- R-TBWT-JDBH → D7 (`project/design/D07.md`)
- R-VB77-BU5O → D1 (`project/design/D01.md`)
- R-VCF3-PLWD → D1 (`project/design/D01.md`)
