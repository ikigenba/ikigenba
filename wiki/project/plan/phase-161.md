# Phase 161 — The composed seam proof: wiki's client against the real prompts binary

*Realizes design Decision 91 (the prompts seam proven against the real binary). Depends on Phase 160.*

Adds the composed-layer test in `internal/llm` that builds the sibling prompts binary from the workspace (`go build` from the repository root so `go.work` resolves it), boots it against a temp database (`PROMPTS_DB_PATH` + `migrate` + `serve --port <free>`), and drives wiki's real `llm.Client` at it: an object-shaped `Context` accepted end to end, echoed byte-verbatim, and deduped on re-Ensure. Keyless by design — items rest `queued`; no provider, no execution. Build/boot failures fail the test loudly with captured stderr, never skip.

**Precondition (run order, not a code dependency):** the sibling `prompts/` tree must already carry its phase 79 (`context` as a raw JSON value) — against the current string-typed decoder this test correctly fails. Run the prompts loop to completion first, exactly as the previous build round did.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim:
  - R-UF1D-AWZ1 — real-binary Ensure accepts an object-`Context` pipeline request (new item, `queued`) and a same-key re-Ensure returns the same id
  - R-UG99-OOPQ — real-binary `Get` returns `Context` byte-identical to what the client sent
- `go test ./...` from `wiki/` is green.
