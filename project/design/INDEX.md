# Suite contracts — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file and carries the **proof-location marker** naming where its tagged
test lives. Resolve an id by grepping this index (or the Decision files directly).
Regenerate this file whenever a Decision is added or its Verification ids change.

Decision numbers are permanent: **D04, D07, D09, D10, D13, D15, and D16 are
retired** (their code-owning content moved to `bin/project/`, `nginx/project/`, and
`opsctl/project/`) and are never reused.

## Decisions

- **D1** → `project/design/D01.md` — The `/opt/<svc>/` install tree — ids: R-3SAU-8T9F, R-LHY1-6IS8, R-VCF3-PLWD (all [proof: opsctl])
- **D2** → `project/design/D02.md` — Versioned release bundle: `libexec/` binary + `etc/<v>`/`share/<v>` + symlink swap, and the bundle filename schema — ids: R-1A79-JG03, R-1BF5-X7QS, R-1CN2-AZHH, R-3TIQ-ML04, R-3UQN-0CQT, R-3VYJ-E4HI (all [proof: opsctl])
- **D3** → `project/design/D03.md` — SemVer 2.0 version identity & ordering — ids: R-3X6F-RW87, R-3YEC-5NYW, R-40U4-X7GA, R-4221-AZ6Z, R-439X-OQXO (all [proof: opsctl])
- **D5** → `project/design/D05.md` — The `state/` ÷ `cache/` backup boundary — ids: R-485J-7TWG [proof: appkit], R-49DF-LLN5 [proof: opsctl]
- **D6** → `project/design/D06.md` — Epoch re-mint by exclusion + boot-reconstruction invariant — ids: R-4BT8-D54J [proof: eventplane], R-4D14-QWV8 [proof: eventplane], R-4E91-4OLX [proof: appkit]
- **D8** → `project/design/D08.md` — Per-service adoption & live-box migration — ids: R-4LKF-FB23 [proof: per-service], R-4MSB-T2SS [proof: opsctl]
- **D11** → `project/design/D11.md` — The env contract: portable authored `manifest.env` + `IKIGENBA_ROOT` path composition + reduced verb set — ids: R-8DF1-W89F [proof: per-service], R-8EMY-A004 [proof: appkit], R-8FUU-NRQT [proof: appkit], R-8H2R-1JHI [proof: appkit], R-8IAN-FB87 [proof: per-service], R-QQNU-T5M7 [proof: appkit], R-VKB6-SHHV [proof: per-service]
- **D12** → `project/design/D12.md` — The per-app customer-data parameter contract — ids: none (an already-live external contract plus `bin/` tooling; verified once outside the loop)
- **D14** → `project/design/D14.md` — The telemetry and correlation contract — ids: none (a prose contract; behaviors proven by the implementing trees)
- **D17** → `project/design/D17.md` — The owner-identity contract: `X-Owner-Id` is the scoping key — ids: none (a prose contract; behaviors proven by the implementing trees)
- **D18** → `project/design/D18.md` — The event-plane contract: envelope, routing keys, and the SSE wire — ids: none (a prose contract; behaviors proven by the implementing trees)
- **D19** → `project/design/D19.md` — The content plane: bytes by reference — ids: none (a prose contract; behaviors proven by the implementing trees)
- **D20** → `project/design/D20.md` — The MCP surface contract: structured results, error vocabulary, and discovery — ids: none (a prose contract; behaviors proven by the implementing trees)
- **D21** → `project/design/D21.md` — Configuration-channel routing: which value goes in which channel, and why — ids: none (a prose contract; enforcement is the channels' own ids and per-service adoption)
- **D22** → `project/design/D22.md` — Library dependency versioning: unversioned in-repo siblings, one suite-wide agentkit pin, a replace-free workspace — ids: R-3R5W-79JK, R-3SDS-L1A9, R-3TLO-YT0Y, R-3UTL-CKRN (all [proof: bin])

## Verification ids → Decision

- R-1A79-JG03 → D2 (`project/design/D02.md`) [proof: opsctl]
- R-1BF5-X7QS → D2 (`project/design/D02.md`) [proof: opsctl]
- R-1CN2-AZHH → D2 (`project/design/D02.md`) [proof: opsctl]
- R-3R5W-79JK → D22 (`project/design/D22.md`) [proof: bin]
- R-3SAU-8T9F → D1 (`project/design/D01.md`) [proof: opsctl]
- R-3SDS-L1A9 → D22 (`project/design/D22.md`) [proof: bin]
- R-3TIQ-ML04 → D2 (`project/design/D02.md`) [proof: opsctl]
- R-3TLO-YT0Y → D22 (`project/design/D22.md`) [proof: bin]
- R-3UQN-0CQT → D2 (`project/design/D02.md`) [proof: opsctl]
- R-3UTL-CKRN → D22 (`project/design/D22.md`) [proof: bin]
- R-3VYJ-E4HI → D2 (`project/design/D02.md`) [proof: opsctl]
- R-3X6F-RW87 → D3 (`project/design/D03.md`) [proof: opsctl]
- R-3YEC-5NYW → D3 (`project/design/D03.md`) [proof: opsctl]
- R-40U4-X7GA → D3 (`project/design/D03.md`) [proof: opsctl]
- R-4221-AZ6Z → D3 (`project/design/D03.md`) [proof: opsctl]
- R-439X-OQXO → D3 (`project/design/D03.md`) [proof: opsctl]
- R-485J-7TWG → D5 (`project/design/D05.md`) [proof: appkit]
- R-49DF-LLN5 → D5 (`project/design/D05.md`) [proof: opsctl]
- R-4BT8-D54J → D6 (`project/design/D06.md`) [proof: eventplane]
- R-4D14-QWV8 → D6 (`project/design/D06.md`) [proof: eventplane]
- R-4E91-4OLX → D6 (`project/design/D06.md`) [proof: appkit]
- R-4LKF-FB23 → D8 (`project/design/D08.md`) [proof: per-service]
- R-4MSB-T2SS → D8 (`project/design/D08.md`) [proof: opsctl]
- R-8DF1-W89F → D11 (`project/design/D11.md`) [proof: per-service]
- R-8EMY-A004 → D11 (`project/design/D11.md`) [proof: appkit]
- R-8FUU-NRQT → D11 (`project/design/D11.md`) [proof: appkit]
- R-8H2R-1JHI → D11 (`project/design/D11.md`) [proof: appkit]
- R-8IAN-FB87 → D11 (`project/design/D11.md`) [proof: per-service]
- R-LHY1-6IS8 → D1 (`project/design/D01.md`) [proof: opsctl]
- R-QQNU-T5M7 → D11 (`project/design/D11.md`) [proof: appkit]
- R-VCF3-PLWD → D1 (`project/design/D01.md`) [proof: opsctl]
- R-VKB6-SHHV → D11 (`project/design/D11.md`) [proof: per-service]
