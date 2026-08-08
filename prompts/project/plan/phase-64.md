# Phase 64 — Seeding: the one-time definition backfill at boot

*Realizes design Decision 54 (seeding). Depends on Phase 63.*

`Service.SeedDefinitions` and its call from the composition root, after
migrations and before the listener opens: assign every missing `name_key`
(suffixing `-2`, `-3` on collision), ensure each repository exists, skip any
that already carries a commit, and batch-commit the three columns for the rest.
Bounded exponential backoff up to 30 seconds on `ErrUnavailable`, then a
non-zero exit naming the version plane.

Nothing is deleted here. The content columns keep their values; Phase 69 is what
retires them, and it must not run before this phase is green.

**Done when:**

- `R-RTAE-HPB2` — the first pass over three prompts seeds three and issues
  three `Commit` calls; a second pass, with the fake reporting a head for each,
  seeds zero and issues zero `Commit` calls while leaving every `name_key` set.
- `R-RUIA-VH1R` — a row with a system prompt seeds exactly three paths whose
  contents equal the columns byte for byte (`config.json` verbatim); a row
  without one seeds two; `"Report"` and `"report!"` receive `report` and
  `report-2`, both persisted.
- `R-RVQ7-98SG` — a plane failing twice then succeeding still completes boot
  with everything seeded; a plane failing throughout makes startup return a
  non-nil error naming the version plane with no listener opened.
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
