# Phase 7 — The end-to-end layer over the real composed service

*Realizes design Decision 7 (test strategy & the end-to-end layer). Depends on
Phase 03 and Phase 05.*

`internal/e2e` stands up the **real** service — the actual `appkit.Spec` from
the composition root, on a real ephemeral `127.0.0.1` listener, over a real
temp-file database with the real migration set — and drives it only through its
two public doors, `POST /ingest` and `POST /mcp`. It reaches inside no package.

The fixture posts a multi-service, multi-hop chain with the shape a real
incident has — a `root`, an `edge`, several `request` records from different
services, an `outbound`, a `publish`/`consume` pair, and a `lifecycle` start —
alongside records from an unrelated chain that must not leak into any answer.
This layer is the only place the ingest codec, the schema, the query builder,
the cursor, and the tool layer are proven to agree.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-WC40-9T5U — a chain posted to `POST /ingest` is afterwards retrievable
    through `POST /mcp`: `chain` returns every record of that chain from every
    service in time order and none from the unrelated chain; a `search`
    narrowed by service and time range returns the expected subset; and `get`
    on an id from those results returns the same record with unchanged `params`
    bytes. The test speaks only HTTP.
  - R-WDBW-NKWJ — after the ingest the served instance is stopped and a new one
    is started over the same database file; `chain` and `get` through the new
    instance return the same records.
- The end-to-end layer touches no package internals other than the record type
  it builds fixture JSON from:
  `grep -rnE 'telemetry/internal/(db|ingest|mcp|retention)' internal/e2e/`
  run from `telemetry/` returns empty (exit 1).
- No test in the layer is skipped: `go test ./internal/e2e/... -v` reports zero
  `--- SKIP` lines.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.
