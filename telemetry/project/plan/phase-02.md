# Phase 2 — The record type, the schema, and the append-only store

*Realizes design Decision 2 (record type, schema, store). Depends on Phase 01.*

Two packages: `internal/record` (the leaf) and `internal/db` (the store over the
chassis's shared `*sql.DB`).

- `internal/record` — the `Record`/`Actor`/`Outcome`/`Kind` types with the exact
  JSON tags D2 states, the closed `Kind` vocabulary, `Validate()` enforcing the
  protocol shape (26-char Crockford id, parseable RFC3339 `time`, known `kind`,
  non-empty `service`/`op`, `correlation_id` required except for `lifecycle`,
  `params`/`detail` objects when present), and `NormalizeTime`/`TimeLayout`.
  `params` and `detail` stay `json.RawMessage` and are never re-encoded.
- `internal/db/migrations/002_records.sql` — the `records` and `ingest_drops`
  tables with the seven `idx_records_*` composite indexes, verbatim from D2.
  This is a greenfield bootstrap file with an integer prefix, matching the
  sibling services; every *later* schema change uses
  `bin/create-migration telemetry <name>`.
- `internal/db` — `Store` with exactly the methods D2 lists: `InsertRecords`
  (idempotent `INSERT OR IGNORE`, one transaction, returns newly-stored count),
  `NoteDropped`, `DroppedTotal`, `Get`, `Chain`, `Search` (keyset paging over
  `(time DESC, id DESC)` with the `Query`/`Cursor` types), `Oldest`, and
  `PruneBefore`. No update method, no delete-by-id, no raw-SQL entry point.
  Timestamps are normalized at this boundary.

Tests open real temp-file SQLite and apply the real migration set through the
appkit runner; the clock is injected.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-V938-15FM — the applied migration set yields the stated columns,
    constraints, `ingest_drops`, and exactly the seven `idx_records_*` indexes
    per `PRAGMA table_info` / `PRAGMA index_list`.
  - R-VAB4-EX6B — `InsertRecords` is idempotent per id and first-write-wins: a
    repeat with a different `op`/`service` leaves the original row and reports
    `stored == 0`.
  - R-VBJ0-SOX0 — normalized timestamps order correctly as text: `…:06.5Z` and
    `…:06.45Z` come back in true chronological order.
  - R-VCQX-6GNP — `Chain` returns every record of one correlation id across
    services, ascending `(time, id)`, excluding other chains, regardless of
    insert order or batch boundaries.
  - R-VDYT-K8EE — keyset paging partitions the result set exactly (no repeat,
    no gap, ties broken by id, unaffected by a newer insert between pages).
  - R-VF6P-Y053 — `EXPLAIN QUERY PLAN` shows a `SEARCH` using the corresponding
    `idx_records_*` index — never `SCAN records` — for each of `service`,
    `kind`, `owner_email`, `client_id`, `sha256`, `correlation_id`, and a bare
    time range.
  - R-VGEM-BRVS — `PruneBefore` removes strictly-older records (boundary
    equality survives), prunes `ingest_drops` on the same boundary, and returns
    the true removed count.
- The store exposes no mutation path beyond insert and prune:
  `grep -nE 'func \(s \*Store\) (Update|Delete|Exec|Raw)' internal/db/*.go`
  run from `telemetry/` returns empty (exit 1).
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.
