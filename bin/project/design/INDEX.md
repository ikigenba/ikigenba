# bin — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolve an id by grepping this index (or the Decision files
directly). Regenerate this file whenever a Decision is added or its Verification
ids change.

## Decisions

- **D1** → `bin/project/design/D01.md` — Version production: `bin/bump` and `bin/ship` — ids: none (bash orchestration in the deliberately-untested tier; consumer-side proof on the box)
- **D2** → `bin/project/design/D02.md` — `bin/push-secrets`: seeding a service's deployed secrets from its `.envrc` — ids: none (bash orchestration around a live cloud API; verified once outside the loop)
- **D3** → `bin/project/design/D03.md` — The local dev stack: `bin/start` / `bin/stop`, and how a service joins it — ids: none (untested orchestration; its staging half's ids belong to D5)
- **D4** → `bin/project/design/D04.md` — `bin/create-migration`: the single mint for a migration version — ids: none (bash in the untested tier; verified once outside the loop)
- **D5** → `bin/project/design/D05.md` — The manifest readers are proven under the green gate (`bin/bintest`) — ids: R-V3XG-PB8R, R-V6D9-GUQ5, R-V7L5-UMGU
- **D6** → `bin/project/design/D06.md` — `bin/bintest` proves the library-dependency contract — ids: none minted here; realizes the umbrella's R-3R5W-79JK, R-3SDS-L1A9, R-3TLO-YT0Y, R-3UTL-CKRN (owned by `root project/design/D22.md`, `[proof: bin]`)
- **D7** → `bin/project/design/D07.md` — The testing-language contract: `bin/bintest` is hermetic-only, and `bin/` gets an `AGENTS.md` — ids: none minted here; cites the umbrella's R-O1AD-MRKW, R-O2IA-0JBL (owned by `root project/design/D23.md`, `[proof: per-service]`)

## Verification ids → Decision

- R-3R5W-79JK → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3SDS-L1A9 → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3TLO-YT0Y → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-3UTL-CKRN → D6 (`bin/project/design/D06.md`; owned by `root project/design/D22.md`)
- R-O1AD-MRKW → D7 (`bin/project/design/D07.md`; owned by `root project/design/D23.md`)
- R-O2IA-0JBL → D7 (`bin/project/design/D07.md`; owned by `root project/design/D23.md`)
- R-V3XG-PB8R → D5 (`bin/project/design/D05.md`)
- R-V6D9-GUQ5 → D5 (`bin/project/design/D05.md`)
- R-V7L5-UMGU → D5 (`bin/project/design/D05.md`)
