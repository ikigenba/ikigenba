# Phase 23 — Outbox schema convergence and the migration-immutability guard

*Realizes design Decision 21 (outbox schema convergence) and its adoption of
root `project/design/D25.md` (migration immutability).*

What exists at the end:

- `internal/db/migrations/20260712201504_outbox_routing.sql` restored to its
  original committed body, byte-identical (sha256
  `571384126d4486bcaa1c123c863146ee548b6a4eb9a5ebee8627514b46847ac4`;
  recoverable via `git show 5e1bd355^:webhooks/internal/db/migrations/20260712201504_outbox_routing.sql`).
- One new timestamped migration, minted with
  `bin/create-migration webhooks outbox_correlation`, whose body is
  `DROP INDEX IF EXISTS idx_outbox_created_at;` +
  `DROP TABLE IF EXISTS outbox;` + `eventplane`'s `outbox.SchemaSQL` verbatim
  — converging the deployed (column-less) and fresh lineages onto one schema,
  which restores inbound `POST /in/<name>` delivery on the deployed lineage
  (D5's durable-before-ack append succeeds again).
- `internal/db/migrations_outbox_test.go` re-pointed: it resolves its
  SchemaSQL-verbatim target as the lexically-greatest migration filename
  containing `outbox` (never a hard-coded name); its `003_outbox.sql`
  frozen-legacy assertion is unchanged (R-A5V4-ANGY's tag stays on it).
- `internal/db/migrations.sha256` — one `<sha256-hex>  <filename>` line per
  embedded migration, sorted by filename — plus the guard test that recomputes
  every embedded migration's hash and compares with total set equality,
  failing by filename on any edit, unmanifested addition, or dangling line.

**Done when:**

- R-NLTJ-K4X4 — a real SQLite database prepared on the deployed lineage (the
  original `20260712201504` body applied and recorded, no `correlation_id`
  column) then migrated with the full current set ends with the
  `outbox.SchemaSQL` column set and a succeeding `outbox.Append` — covered by
  a tagged test.
- R-NN1F-XWNT — the embedded `20260712201504_outbox_routing.sql` hashes to
  `571384126d4486bcaa1c123c863146ee548b6a4eb9a5ebee8627514b46847ac4` — covered
  by a tagged test.
- R-NFQ1-NA7N — the `migrations.sha256` manifest is total and true per the
  root D25 contract — covered by a tagged test.
- The suite is green per design Conventions (`cd webhooks && go build ./... &&
  go vet ./... && go test ./...`).
