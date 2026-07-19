# Phase 37 — The `calls` store: schema, filters, aggregation, body retention

*Realizes design Decision 28 (the `calls` table).*

Build the `internal/calls` package and its migration: the suite-wide inference record. Mint the migration with `bin/create-migration prompts create-calls` (never hand-numbered) carrying D28's schema and indexes. Implement `Store` over the shared appkit DB handle — `Insert`, `InsertTx`, `Get`, `List` (filter + pagination), `Aggregate` (group by name/origin/model/day), `PruneBodies` — plus the Row/Filter/GroupBy/Bucket types, the class constants, and the origin/name grammar validators (exported for D29/D30's envelope checks). Wire the retention sweep into `cmd/prompts`: `PROMPTS_CALLS_BODY_RETENTION_DAYS` (default 30) read from env, surfaced in `ManifestExtras`, swept at boot and on a 24h ticker.

**Done when:** the suite is green (`go build ./...`, `go test ./...`, `gofmt -l .` empty from `prompts/`) and these ids are covered by clearly-named tests tagged verbatim:

- R-5J1W-8BCM — full-field insert/Get round-trip
- R-5K9S-M33B — prune nulls old bodies, metrics unchanged
- R-5LHO-ZUU0 — prune leaves young bodies intact
- R-5MPL-DMKP — List filter dimensions + pagination
- R-5NXH-REBE — Aggregate sums by name within a window
