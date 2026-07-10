# Phase 15 — The durable upload queue (schema + coalescing store)

*Realizes design Decision 17, the queue slice (R-KC2V-JPR1, R-KDAR-XHHQ). Depends
on phase 11 for the migration runner; independent of phases 12–14. Prerequisite
for the client (16) and uploader (17).*

Observable end state:

- A new additive migration `internal/db/migrations/005_upload_queue.sql` (created
  with `bin/create-migration dropbox upload_queue`) adds the `upload_queue` table
  keyed by `path` (PRIMARY KEY → one pending op per path), with `op`
  (`put|mkdir|delete|move`), `dest`, `origin`, `enqueued_at`, `attempts`,
  `next_attempt_at`, `state` (`pending|failed`), `last_error`, plus the
  `(state, next_attempt_at)` due-index. Frozen `001`–`004` untouched.
- `internal/dropbox/store.go` gains the queue surface: `EnqueueUpload(tx, row)`
  performing a per-path **upsert** (latest op wins → coalescing), `DueUploads`
  (state `pending` and `next_attempt_at <= now`), `ClearUpload(tx, path)`, and
  `FailUpload(tx, path, err, nextAttempt)`; plus backlog counters
  (`pending_uploads`, `failed_uploads`, oldest pending) for health (17).

**Done when:** the suite is green (design Conventions commands +
`bin/check-migrations dropbox`, from `dropbox/`) and:

- R-KC2V-JPR1 is covered by a test asserting two enqueues for the same path leave
  exactly one `upload_queue` row carrying the latest op (PK upsert coalescing).
- R-KDAR-XHHQ is covered by a test asserting `005_upload_queue` is recorded in
  `schema_migrations` and the table enforces one row per path.
