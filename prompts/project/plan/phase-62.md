# Phase 62 — The name key: slug, schema, and uniqueness

*Realizes design Decision 51 (definition tree layout and repo name key).
Depends on Phase 61.*

`prompt.NameKey` (pure function, no database), a new **additive** timestamped
migration adding `prompts.name_key` with a unique index over it and
`runs.definition_sha`, and the store/service plumbing that derives, persists,
and enforces the key on `create` and on a renaming `update`. No plane calls are
made in this phase — the key exists before anything uses it.

Mint the migration with `bin/create-migration prompts <name>`; never hand-number
it, and never touch a committed migration. `ALTER TABLE … ADD COLUMN` only: the
existing rows are production data and must all survive.

**Done when:**

- `R-RDFP-IOO1` — `NameKey` collapses runs of non-`[a-z0-9]` to a single `-`,
  trims the ends, caps at 64 bytes with no trailing `-`, and falls back to the
  supplied id when nothing alphanumeric remains.
- `R-RENL-WGEQ` — a second prompt whose name slugs onto an existing key is
  rejected with a `*ValidationError` naming the existing prompt and the key, no
  row is inserted, and the same holds for a renaming `update` and across two
  distinct owner ids.
- `R-RFVI-A85F` — the full embedded migration set applied to a real SQLite
  database already holding prompt and run rows yields `prompts.name_key` with a
  unique index and `runs.definition_sha`, with every pre-existing row's id and
  content intact (counts and ids compared before and after).
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
