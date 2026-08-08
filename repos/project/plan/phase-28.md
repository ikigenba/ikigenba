# Phase 28 — Data model: the v2 schema, the rebuild migration, and the store

*Realizes design Decision 2 (data model & migrations). Depends on Phase 27.*

`internal/db/migrations/` gains **one** new timestamped migration (created with
`bin/create-migration repos <name>`; no committed migration is edited) that
drops the v1 `repos`, `sessions`, and `feed_offset` tables and creates
`repositories`, `statuses`, and `run_tokens` as D2 specifies. The outbox
migration and its DDL drift guard stay.

`internal/repos/store.go` is rebuilt over the shared single-writer handle with
the v2 value objects (`Repository`, `Status`, `RunToken`) and the query set D2
names, every list scoped on `owner_id`, and every mutating method taking a
`*sql.Tx` so the D23 outbox append rides the same transaction.

**Done when:** `go build ./...`, `go vet ./...`, `go test ./...` exit 0 and
`gofmt -l .` prints nothing, and these ids are each covered by a clearly-named
test —

- R-IWEY-SUZH — the full embedded migration set over a fresh real SQLite yields
  exactly the four v2 tables with the stated columns and indexes, no `repos`,
  `sessions`, or `feed_offset` table, and the outbox drift guard holds.
- R-IYUR-KEGV — the same set applied over a seeded v1 database drops those three
  tables and leaves the v2 tables present and empty, without error.
- R-J02N-Y67K — `Repository`, `Status`, and `RunToken` round-trip; `SetStatus`
  is an upsert on its four-column key; `ListStatuses` of an unseen sha is empty;
  `SweepExpiredTokens` deletes only expired rows.
- R-J1AK-BXY9 — a state change plus its outbox append are one transaction: a
  forced rollback leaves neither.
- R-J2IG-PPOY — two owners sharing an `owner_email` with distinct `owner_id`
  see only their own repositories; archived rows appear in neither listing.
