# Phase 43 — Outbox schema convergence and the migration-immutability guard

*Realizes design Decision 41 (outbox schema convergence) and its adoption of
root `project/design/D25.md` (migration immutability).*

What exists at the end:

- `internal/db/migrations/20260712192242_outbox_routing.sql` restored to its
  original committed body, byte-identical (sha256
  `9348de0c347feaa2f9ce74c90d53c9fcf17eed56fd66538e528c8f729b46eb38`;
  recoverable via `git show 1e26ba16^:scripts/internal/db/migrations/20260712192242_outbox_routing.sql`).
- One new timestamped migration, minted with
  `bin/create-migration scripts outbox_correlation`, whose body is
  `DROP INDEX IF EXISTS idx_outbox_created_at;` +
  `DROP TABLE IF EXISTS outbox;` + `eventplane`'s `outbox.SchemaSQL` verbatim
  — converging the deployed (column-less) and fresh lineages onto one schema.
- `internal/db/migrations_outbox_test.go` re-pointed: it resolves its
  SchemaSQL-verbatim target as the lexically-greatest migration filename
  containing `outbox` (never a hard-coded name); its `004_outbox.sql`
  frozen-legacy assertion is unchanged (R-84Q9-6QMD's tag stays on it).
- `internal/db/migrations.sha256` — one `<sha256-hex>  <filename>` line per
  embedded migration, sorted by filename — plus the guard test that recomputes
  every embedded migration's hash and compares with total set equality,
  failing by filename on any edit, unmanifested addition, or dangling line.

**Done when:**

- R-NGXY-11YC — a real SQLite database prepared on the deployed lineage (the
  original `20260712192242` body applied and recorded, no `correlation_id`
  column) then migrated with the full current set ends with the
  `outbox.SchemaSQL` column set and a succeeding `outbox.Append` — covered by
  a tagged test.
- R-NJDQ-SLFQ — the embedded `20260712192242_outbox_routing.sql` hashes to
  `9348de0c347feaa2f9ce74c90d53c9fcf17eed56fd66538e528c8f729b46eb38` — covered
  by a tagged test.
- R-NFQ1-NA7N — the `migrations.sha256` manifest is total and true per the
  root D25 contract — covered by a tagged test.
- The suite is green per design Conventions (`cd scripts && go build ./... &&
  go vet ./... && go test ./...`, `gofmt -l .` silent).
