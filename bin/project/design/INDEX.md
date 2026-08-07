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

## Verification ids → Decision

- R-V3XG-PB8R → D5 (`bin/project/design/D05.md`)
- R-V6D9-GUQ5 → D5 (`bin/project/design/D05.md`)
- R-V7L5-UMGU → D5 (`bin/project/design/D05.md`)
