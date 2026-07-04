# Phase 2 — DB migration: backfill provider and model aliases

*Realizes design Decision 8 (DB migration). Depends on Phase 01.*

**`prompts/internal/db/migrations/`**: add one new timestamped SQL file created with `bin/create-migration prompts backfill-provider-and-model-aliases`. The file contains two idempotent backfills in a single forward-only script:

1. Set `$.provider` to `"anthropic"` for every prompt row where it is `NULL` or `""`.
2. Replace the four known short aliases (`opus`, `sonnet`, `haiku`, `pro`) in `$.model` with their canonical bare IDs (`claude-opus-4-7`, `claude-sonnet-4-6`, `claude-haiku-4-5`, `gemini-3.1-pro-preview`).

No columns are added or removed; no table is reshaped. Only the JSON column values change.

**Done when:** R-KBLR-VBHQ and R-KCTO-938F are each covered by a clearly-named test (run migration against an in-memory SQLite database, assert the post-state) and `go test ./...` is green.
