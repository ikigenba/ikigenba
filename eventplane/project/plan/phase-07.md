# Phase 7 — Correlation on the producer path (`outbox`)

*Realizes design Decision 7 (producer correlation), with the D1 amendments it
implies. Depends on Phase 6.*

`outbox` carries the chain. `SchemaSQL` declares `correlation_id TEXT NOT NULL
DEFAULT ''` last (D1's DDL block); a new exported constant
`AddCorrelationIDSQL` holds the `ALTER TABLE` text services apply to an
existing outbox. `Append` becomes `Append(ctx context.Context, tx *sql.Tx, ev
Event) error`, reads `correlation.FromContext(ctx)`, inserts it (via
`tx.ExecContext`, so a cancelled context aborts the write), and mints nothing
when the context carries no id. `eventRow`/`fetch` carry the column, and the
`envelope` gains `correlation_id` — always present, `""` when uncorrelated —
between `subject` and `payload`. `outbox.Event` gains no field. In-module
callers and tests are updated for the new signature; consuming services adopt
it in their own specs.

The existing test for R-3D34-SZYT (exact envelope key set) is amended to the
revised key list from D1 — `id`, `source`, `time`, `kind`, `subject`,
`correlation_id`, `payload` — and must assert the set exactly, both for a
correlated and an uncorrelated event.

**Done when:**

- `go test ./...` and `go vet ./...` from `eventplane/` both exit 0, and
  `gofmt -l .` prints nothing.
- These behaviors are covered by clearly-named tests in `eventplane/outbox/`,
  each citing its id, and each DDL/wire claim exercised on the real substrate
  (a real `modernc.org/sqlite` database; the real `FeedHandler()` in an
  `httptest.Server` over a real HTTP client):
  - R-UJ7Y-E4QY — `SchemaSQL` applies to real SQLite; `PRAGMA table_info`
    yields exactly `seq, event_id, kind, subject, payload, created_at,
    correlation_id` in order, `correlation_id` NOT NULL defaulting to `''`;
    a `sqlite_sequence` row for `outbox` exists after one insert.
  - R-UKFU-RWHN — legacy table (DDL embedded in the test) + `AddCorrelationIDSQL`
    yields a `PRAGMA table_info` identical to a fresh `SchemaSQL` table, and
    the pre-existing row reads `correlation_id = ''`.
  - R-ULNR-5O8C — the upgrade preserves `seq` values and `MAX(seq)`, and a
    post-upgrade `Append` gets `MAX(seq)+1` even after all pre-existing rows
    are deleted.
  - R-UMVN-JFZ1 — `Append` under `correlation.WithContext(ctx, X)` stores
    exactly `X`; under a bare context stores exactly `""`.
  - R-UO3J-X7PQ — the served frame's `data:` JSON carries `"correlation_id"`
    equal to `X`, and `""` for an uncorrelated event.
  - R-UPBG-AZGF — `Append` with an already-cancelled context returns a
    non-nil error and inserts no row.
- R-3D34-SZYT's existing test asserts the revised exact key set (seven keys,
  no `type`) for both a correlated and an uncorrelated event.
- `eventplane/go.mod` gains no `require` line:
  `git diff -- go.mod | grep -c '^+.*require'` is `0`.
