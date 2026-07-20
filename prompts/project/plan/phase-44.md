# Phase 44 — Browse queries: paged, counted, filtered list methods on the domain stores

*Realizes design Decision 35 (browse UI), store-query slice only.*

The `internal/prompt` store gains the two browse queries the UI pages will
consume, and `internal/calls` gains the per-run lookup:

- `BrowseFilter{ Q, Status, PromptID string; Limit, Offset int }` plus
  `Store.BrowsePrompts(ctx, f) ([]Prompt, int, error)` — `updated_at DESC`,
  case-insensitive substring `Q` over name/owner_email, returning the page and
  the filtered total; Status/PromptID ignored.
- `Store.BrowseRuns(ctx, f) ([]Run, int, error)` — `started_at DESC`, exact
  `Status`, exact `PromptID`, substring `Q` over prompt_name/owner_email, each
  independently and combined, with the filtered total.
- `calls.Store.ListByGroup(ctx, groupID) ([]Row, error)` — that group's rows
  only, `started_at ASC`, bodies included.

No handler, template, or nginx change in this phase; the existing
owner-scoped/unpaged MCP list methods are untouched. End state: the three
methods exist with tagged tests over seeded SQLite DBs.

**Done when:** the suite is green (design Conventions: `go build ./...`,
`go test ./...`, `gofmt -l .` empty, from `prompts/`) and these ids are covered
by clearly-named tagged tests:

- R-ZZVE-DK4O — `BrowsePrompts` ordering, substring filter over name/owner,
  paging, and correct filtered total.
- R-013A-RBVD — `BrowseRuns` ordering and each filter dimension independently
  and combined, with paging and correct filtered total.
- R-03J3-IVCR — `ListByGroup` returns exactly the group's rows, `started_at
  ASC`, bodies present.
