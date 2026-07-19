# Phase 94 — The retirement sweep: `llm_calls`, recorder stack, eval harness, agentkit

*Realizes design Decision 63 (the retirement), Decision 1 (the dependency swap's module-graph proof), and the reduced-surface slices of D10 (R-MUQ4-K1JS), D16 (R-3G73-064M rewording), D57 (R-YF06-03HO), and D61/D53 tool-count texts. Depends on Phase 91, Phase 92, and Phase 93.*

Delete everything the conversion obsoletes, leaving no orphaned fragments: the `llm_calls` table (new `bin/create-migration wiki drop_llm_calls` migration), `LLMCallStore`, the `Recorder`/`CallRecord` seam, `WithJobID`, `recording_embedder`, the `llm_calls` MCP tool (surface drops to 12 domain tools; instructions and `guide` re-pointed at prompts' `calls`/`usage`), `cmd/eval-extract`, `internal/eval`, `testdata/eval`, the `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` config reads, and the agentkit `require` in `go.mod`. `extract.DefaultPromptInstructions` stays exported; `WithPromptInstructions` goes with its only consumer. Existing tests asserting the old surface (tool counts, llm_calls behaviors) are updated or deleted along with their dead requirement tags (the tags for D13/D16-llm_calls/D20–D24/D37 ids no longer minted by design).

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-1BRG-F9TN — the migrated schema has no `llm_calls` table; prior migrations byte-identical.
- R-1CZC-T1KC — `tools/list` has no `llm_calls`; instructions/guide no longer offer it as a wiki capability.
- R-1E79-6TB1 — eval/harness paths absent; recorder identifiers absent from non-test source; `DefaultPromptInstructions` still exported.
- R-1GN1-YCSF — no Go source under `wiki/` (excluding `project/`) mentions `ikigenba/agentkit`.
- R-0UOV-2HFX — a `GOWORK=off` production-shaped build succeeds with agentkit absent from `go mod graph`.
- R-0VWR-G96M — `go.mod` carries no agentkit require/replace; only the three sibling replaces.

Additionally deterministic: `grep -rn "ikigenba/agentkit" --include='*.go' .` from `wiki/` (excluding `project/`) exits empty, and the updated membership tests for R-MUQ4-K1JS / R-YF06-03HO pass at fourteen verbs / fifteen tools.
