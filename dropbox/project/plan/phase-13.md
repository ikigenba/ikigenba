# Phase 13 — First-class directories in the index

*Realizes design Decision 15 (directories as real index entries). Depends on
phase 11 for the settled tree and the appkit migration runner; independent of
phase 12.*

Observable end state:

- A new additive migration `internal/db/migrations/004_directories.sql` (created
  with `bin/create-migration dropbox directories` — timestamped, never
  hand-numbered) adds the `directories` table (`path` PK, `path_lower`,
  `updated_at`) and its `path_lower` index. The frozen `001`–`003` are untouched.
- `internal/dropbox/store.go` gains `UpsertDir`, `DeleteDirSubtree` (recursive,
  returning the removed file + dir paths), and `RenameDirSubtree`, all `*sql.Tx`
  methods keyed on folded paths.
- `internal/dropbox/service.go` gains `Stat(path) (Entry, error)` resolving a path
  to a file **or** directory (`Kind ∈ {file,dir}`, `not_found` when absent), and
  `List` reports directories interleaved with files, ordered by path. Recursive
  `Delete` of a directory removes the directory rows and every file beneath in
  one tx, emitting one `file.deleted` per removed file. Implicit parent
  directories created on a file write are indexed as directory rows.
- No `file.*` event is emitted for a bare `mkdir`/`rmdir` (directories are
  structural).

**Done when:** the suite is green (design Conventions commands +
`bin/check-migrations dropbox`, from `dropbox/`) and:

- R-JZVV-Q0C3 — a test asserting `mkdir` of an empty path makes it appear in
  `Stat` (`Kind:dir`) and `List` with no file under it.
- R-K13S-3S2S — a test asserting recursive `Delete` of a directory removes the
  directory row(s) and every file beneath, emitting exactly one `file.deleted`
  per removed file, in one tx.
- R-K2BO-HJTH — a test asserting a directory move reparents the directory and its
  whole subtree (old paths absent, new paths present, folds updated).
- R-K3JK-VBK6 — a test asserting `004_directories` is recorded in
  `schema_migrations`, the `directories` table exists, and its `path_lower` fold
  prevents a case-only dir/file collision.
