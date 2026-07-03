# Phase 2 — The service table with typed blocks and guardrail tests

*Realizes design Decision 2 (the service table), ids `R-B00K-9JYR`, `R-B18G-NBPG`,
`R-B2GD-13G5`, `R-B3O9-EV6U`. Depends on Phase 1 (the module exists).*

## What gets built

Package `registry`, in `registry/registry.go`:

- The `Block` type and its four constants (`Core`, `Apps`, `Connectors`, `Custom`),
  and `func (b Block) Range() (lo, hi int)` returning the inclusive port range each
  block owns (Core 3000–3099, Apps 3100–3199, Connectors 3200–3299, Custom
  3000–3099).
- The `Service` struct (`Name string`, `Port int`, `Block Block`).
- The frozen `var Services []Service` seed table exactly as listed in D2:
  dashboard 3000, wiki 3001, prompts 3002, scripts 3003, sites 3004, cron 3005,
  webhooks 3006 (Core); crm 3100, ledger 3101 (Apps); dropbox 3200, notify 3201,
  gmail 3202, github 3203 (Connectors).

Tests in `registry/registry_test.go` (package-local, named for the behavior),
iterating `Services` — data-driven, not a hardcoded expectation per row.

Observable end state: the authoritative table exists and its guardrails are
enforced by passing tests; introducing a duplicate name/port, an out-of-block
port, or moving `dashboard` off 3000 would make a test fail.

## Done when

All of the following hold on identical repo state, from the module root
(`registry/`):

- `GOWORK=off go build ./...` exits 0.
- `GOWORK=off go test ./...` exits 0 with no failures and no `SKIP`.
- Each id is covered by a genuinely-asserting, package-local
  `registry/*_test.go` test tagged with a `// R-XXXX-XXXX` comment line and named
  for the behavior; `grep -rE 'R-B00K-9JYR|R-B18G-NBPG|R-B2GD-13G5|R-B3O9-EV6U' registry --include='*_test.go'`
  returns ≥ 4 matching lines, asserting:
  - `R-B00K-9JYR` — no two rows share a `Port` (a deliberate duplicate would fail).
  - `R-B18G-NBPG` — no two rows share a `Name`.
  - `R-B2GD-13G5` — every row's `Port` is within its `Block.Range()` (an
    out-of-block port fails; a mere `Port > 0` check does not satisfy this).
  - `R-B3O9-EV6U` — `dashboard` is in the table with `Port == 3000` (any other
    value fails).
