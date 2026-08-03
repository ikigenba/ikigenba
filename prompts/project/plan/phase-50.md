# Phase 50 — The two prompt loaders, and a run listing that outlives its prompt

*Realizes design Decision 40 (two loaders; `RunList` fix). Depends on Phase 49.*

**End state.**

Package `prompt` exports `Executed{Name, UserPrompt, SystemPrompt, Config}` and three loaders: `Service.LoadFromPrompt(ctx, ownerID, promptID)` reading the live `prompts` row, the package-level `LoadFromRun(runsDir, runID)` reading `<runsDir>/<run_id>/input/`, and `Service.RunExecuted(ctx, ownerID, runID)` which resolves the run owner-scoped and then calls `LoadFromRun`.

Spawn is the only caller of `LoadFromPrompt`: it loads the live prompt, writes the snapshot into `input/`, and inserts the `runs` row. `runner.execute` replaces its hand-rolled `input/` reads with `LoadFromRun`, keeping its separate `event.json` read. Every read of a past run goes through `RunExecuted`, and the run's executed prompt text and config are reachable from the MCP surface and the browse UI.

`Service.RunList` no longer calls `GetPrompt`. It queries `runs` by `prompt_id` scoped on `owner_id`, so a deleted prompt's runs stay listable; an unknown or unowned `prompt_id` yields an empty list, not an error.

No run-side read path references the `prompts` table.

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, and `gofmt -l .` is silent.
- These ids are covered by clearly-named tests:
  - R-ZOYX-Y9WA — after editing a prompt post-run, `RunExecuted` returns the original text and config while `LoadFromPrompt` returns the updated ones, asserted together.
  - R-ZREQ-PTDO — after deleting a run's prompt, `RunList` for that `prompt_id` and owner still returns the run, and `RunExecuted` still returns what it executed.
  - R-ZSMN-3L4D — `RunExecuted` with a mismatched owner id returns not-found for an existing run with files present; the true owner receives the content.
