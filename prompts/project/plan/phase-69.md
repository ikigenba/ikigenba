# Phase 69 — Retire the content columns

*Realizes design Decision 59 (retiring the content columns). Depends on
Phase 68 — and, critically, on Phase 64 (seeding) having been built and run:
this phase removes the only other copy of every prompt's definition.*

One new **additive-in-spirit** timestamped migration doing three
`ALTER TABLE prompts DROP COLUMN`s (`user_prompt`, `system_prompt`,
`config_json`). SQLite drops the columns in place; the table is not rebuilt and
no row is copied or lost. The `Prompt` struct and the `Store` write/read methods
drop the three fields with them, leaving metadata; the definition travels as
`Executed` from the plane (`Get`/`LoadFromPrompt`) or the run's clone
(`RunExecuted`).

Mint the migration with `bin/create-migration prompts <name>` and do not touch a
committed one.

**Done when:**

- `R-SE0O-ZSWV` — after the full migration set over a database holding three
  prompt rows, `PRAGMA table_info(prompts)` reports none of the three columns
  and all three rows are still present with ids, names, name keys, source paths,
  and timestamps unchanged.
- `R-SF8L-DKNK` — `Get` returns the definition through exactly one
  `Read(nameKey, "main")` on the recording fake, and a source scan finds
  `user_prompt`, `system_prompt`, and `config_json` in no non-test Go source
  (excluding `project/` and the frozen migration files).
- `go build ./...` and `go test ./...` from `prompts/` are green;
  `gofmt -l .` is empty.
