# Phase 108 — The analysis prompt as data: `eval/analysis/` workspace files and the production seams

*Realizes design Decision 69 (analysis eval workbench — prompt-as-data slice).*

The ask question-analysis prompt moves out of `internal/ask/prompts.go` into committed data, exactly as extract's did (D64 pattern, D69 statement): `eval/analysis/prompt.txt` carries today's `analysisPrompt` instruction text verbatim; `eval/analysis/prompt.go` (package `analysisprompt`) embeds it; `internal/ask` exports `DefaultAnalysisInstructions` initialized from it, plus the two production-faithful seams `RenderAnalysis(instructions, question string) string` (the exact assembly `Analyze` sends) and `NormalizeAnalysis(*wiki.QueryAnalysis)` (the exact production normalization: trim, case-insensitive dedupe, sub_queries capped at 4). `Analyze` itself now calls through these seams — same call path, byte-identical prompt, unchanged production behavior. `eval/analysis/config.json` is committed with the D69 reference blocks: the `eval` block mirroring the production ask-subject call site (provider `anthropic`, model `claude-sonnet-4-6`, `max_tokens` 16384, `max_parse_retries` 2), the pinned `embedding` block identical to extract's, and `weights` `{"sub_queries": 0.50, "keywords": 0.30, "aliases": 0.20}`.

This phase touches only `eval/analysis/` (new files) and `internal/ask`; the loader that parses the new config arrives in Phase 109.

**Done when:**
- R-BICU-BZG6 — `ask.DefaultAnalysisInstructions` is byte-identical to the contents of `eval/analysis/prompt.txt` (test reads the file and compares).
- R-BJKQ-PR6V — the prompt the production `Analyze` path sends (captured via a fake D5 client) is byte-identical to `ask.RenderAnalysis(DefaultAnalysisInstructions, question)` for the same question, and production's normalization of a raw `QueryAnalysis` (whitespace, duplicate case-variants, >4 sub_queries) equals `ask.NormalizeAnalysis` applied to the same value.
- `eval/analysis/prompt.txt`, `eval/analysis/prompt.go`, and `eval/analysis/config.json` exist with the contents above, and the suite is green (`go test ./...` from `wiki/`).
