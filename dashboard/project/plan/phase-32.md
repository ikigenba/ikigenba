# Phase 32 — `owner_id` foreign keys: rebuild the four auth-artifact carriers with `REFERENCES identities(id)`

*Realizes design Decision 24 (`owner_id` becomes a real foreign key into `identities`).*

One new forward-only migration, created with
`bin/create-migration dashboard add_owner_id_foreign_keys`, rebuilds
`web_sessions`, `oauth_authcodes`, `oauth_chains`, and `personal_tokens` so
each declares `owner_id TEXT NOT NULL REFERENCES identities(id)` (default
`NO ACTION` — no `ON DELETE` clause). Each rebuild copies every existing row
with an explicit column list, drops the old table, renames the new one into
place, and recreates the existing indexes
(`idx_web_sessions_owner_email`, `idx_oauth_chains_owner_email`,
`idx_oauth_chains_client_id`, `idx_personal_tokens_owner`). No purge: live
rows survive byte-equal. No Go changes. End state: a migrated database rejects
any auth artifact whose `owner_id` does not name an existing identity, and
rejects deleting an identity that still owns artifacts.

**Done when:**

- R-HYL8-V30T — migrating a real SQLite DB pre-seeded with identities and rows
  in all four carriers completes without error and preserves every pre-seeded
  row byte-equal (counts and column values), covered by a genuine test.
- R-HZT5-8URI — post-migration, each of the four carriers rejects an `INSERT`
  whose `owner_id` references no `identities` row with a real SQLite
  foreign-key constraint error, covered by a genuine test.
- R-I111-MMI7 — post-migration, deleting an `identities` row with a dependent
  artifact fails with a foreign-key constraint error and both rows survive,
  while deleting a dependent-free identity succeeds, covered by a genuine test.
- The suite is green per design Conventions: `cd dashboard && go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed.
