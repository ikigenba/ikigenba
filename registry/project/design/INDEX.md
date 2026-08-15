# registry — Design Index

Each Decision maps to its `DNN.md`; every `R-XXXX-XXXX` id maps to its
Decision/file. Resolving an id is a grep against this index (or the Decision files
directly). Regenerate this manifest whenever a Decision is added or its
Verification ids change.

## Decisions

- D1 → `project/design/D01.md` — A standalone, zero-dependency `registry` module at the repo root — none (structural)
- D2 → `project/design/D02.md` — The service table: slice of structs with typed blocks and frozen seeds — owns `R-B00K-9JYR`, `R-B18G-NBPG`, `R-B2GD-13G5`, `R-B3O9-EV6U`, `R-ZNFW-ORR6`
- D3 → `project/design/D03.md` — The resolution API: name → port, name → base URL, loud on unknown — owns `R-B642-6EO8`, `R-B7BY-K6EX`, `R-B8JU-XY5M`, `R-B9RR-BPWB`
- D4 → `project/design/D04.md` — Adopt the suite testing-language contract (`root project/design/D23.md`): hermetic-only layers, `GOWORK=off` mode, no preconditions — owns none locally; **cites** `R-O1AD-MRKW`, `R-O2IA-0JBL`
- D5 → `project/design/D05.md` — Adopt the suite lint contract (`root project/design/D30.md`) at tier `strict` — none (structural; the contract carries no per-service ids)

## Verification ids → Decision

- R-B00K-9JYR → D2 — `project/design/D02.md`
- R-B18G-NBPG → D2 — `project/design/D02.md`
- R-B2GD-13G5 → D2 — `project/design/D02.md`
- R-B3O9-EV6U → D2 — `project/design/D02.md`
- R-B642-6EO8 → D3 — `project/design/D03.md`
- R-B7BY-K6EX → D3 — `project/design/D03.md`
- R-B8JU-XY5M → D3 — `project/design/D03.md`
- R-B9RR-BPWB → D3 — `project/design/D03.md`
- R-O1AD-MRKW → D4 — `project/design/D04.md` (cited from `root project/design/D23.md`, `[proof: per-service]`)
- R-O2IA-0JBL → D4 — `project/design/D04.md` (cited from `root project/design/D23.md`, `[proof: per-service]`)
- R-ZNFW-ORR6 → D2 — `project/design/D02.md`

## Success criteria → ids

Each product success criterion (`project/product/README.md`, in order) mapped
to the id(s) whose tests most directly prove it; the mapping's completeness is
this manifest's concern, each proof's quality the audit's. Regenerated with the
rest of the index.

1. Any known name resolves to the correct port and to `http://127.0.0.1:<port>`,
   with `dashboard` at `3000` →
   R-B642-6EO8, R-B9RR-BPWB, R-B3O9-EV6U
2. An unknown name never returns a usable-looking wrong answer — failed lookup,
   loud strict/URL forms →
   R-B7BY-K6EX, R-B8JU-XY5M
3. Guardrails hold (no duplicate names or ports, every port in its block); a
   deliberate violation fails the tests →
   R-B00K-9JYR, R-B18G-NBPG, R-B2GD-13G5
4. Builds and tests green in isolation with no third-party dependencies →
   R-O1AD-MRKW, R-O2IA-0JBL
