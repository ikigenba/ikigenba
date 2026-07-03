# registry — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolving an id is a grep against this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — A standalone, zero-dependency `registry` module at the repo root — none (structural)
- D2 → `project/design/D02.md` — The service table: slice of structs with typed blocks and frozen seeds — owns `R-B00K-9JYR`, `R-B18G-NBPG`, `R-B2GD-13G5`, `R-B3O9-EV6U`
- D3 → `project/design/D03.md` — The resolution API: name → port, name → base URL, loud on unknown — owns `R-B642-6EO8`, `R-B7BY-K6EX`, `R-B8JU-XY5M`, `R-B9RR-BPWB`

## Verification ids → Decision

- R-B00K-9JYR → D2 — `project/design/D02.md`
- R-B18G-NBPG → D2 — `project/design/D02.md`
- R-B2GD-13G5 → D2 — `project/design/D02.md`
- R-B3O9-EV6U → D2 — `project/design/D02.md`
- R-B642-6EO8 → D3 — `project/design/D03.md`
- R-B7BY-K6EX → D3 — `project/design/D03.md`
- R-B8JU-XY5M → D3 — `project/design/D03.md`
- R-B9RR-BPWB → D3 — `project/design/D03.md`
