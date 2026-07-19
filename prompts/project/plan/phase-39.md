# Phase 39 — `POST /complete`

*Realizes design Decision 29 (the synchronous one-shot completion endpoint). Depends on Phase 37 and Phase 38.*

Build the `internal/inference` package's completion executor and HTTP handler, and mount `POST /complete` through the chassis loopback guard in `cmd/prompts` (beside `/run-content`). Export D3's validation as `prompt.ValidateConfig` and apply it plus the envelope checks (origin/name grammar via `internal/calls`, messages shape, size cap). Execution: catalog resolve → `provider.Build` → `admit.AcquireCall` → tool-less `agentkit.Conversation` with `History` → drain → record one `completion` row via `calls.Store` → respond per D29's status taxonomy. Injectable provider-factory seam for tests.

**Done when:** the suite is green and these ids are covered by tagged tests driving the real handler:

- R-5P5E-5623 — 200 happy path with text, usage, catalog-priced cost
- R-5QDA-IXSS — one `completion` row with attribution and both bodies
- R-5ST3-AHA6 — catalog/reasoning validation → 400, no row
- R-5U0Z-O90V — origin/name/messages envelope validation → 400, no row
- R-5V8W-20RK — multi-turn history reaches the provider intact
- R-5WGS-FSI9 — provider failure → 502, row records the error
- R-5XOO-TK8Y — calls-insert failure → 500
- R-5YWL-7BZN — loopback-guarded, no identity header required
