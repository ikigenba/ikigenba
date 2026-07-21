# Phase 110 — The sibling runner: `cmd/eval-analysis` over shared agentkit plumbing

*Realizes design Decision 69 (analysis eval workbench — runner slice). Depends on Phase 109.*

The agentkit plumbing `cmd/eval-extract` built — provider construction over the five providers, `EVAL_` alias precedence, key/sub auth, the retry-on-invalid loop — is factored into `internal/eval` (inside D63's confinement, whose statement now names `cmd/eval-analysis` among the permitted importers), and `cmd/eval-analysis` is built as a thin sibling composition root: D66's CLI surface verbatim (`run`/`compare`, same flags, split/repeat semantics, stderr progress contract, atomic-write failure doctrine, exit codes), with the call path `ask.RenderAnalysis` → JSON-parse into `wiki.QueryAnalysis` → `ask.NormalizeAnalysis` → `ScoreAnalysisCase`. `cmd/eval-extract` consumes the factored plumbing with behavior bit-for-bit unchanged — all of D66's existing tagged tests keep passing without edits to their assertions. A live smoke build-tagged `eval_live` mirrors D66's.

**Done when:**
- R-BTBX-RX4F — `eval-analysis run` end-to-end against fakes scores every dev case, writes a scorecard matching `ScoreAnalysisCase`, exits 0, and re-prompts a once-unparseable response.
- R-BUJU-5OV4 — unreadable prompt, malformed gold case, and retries-exhausted chat call each exit non-zero naming the cause with no scorecard left at `-out`.
- R-BVRQ-JGLT — one stderr progress line per case×repeat; stdout and the scorecard stay clean of progress text.
- R-BWZM-X8CI — `EVAL_` alias precedence for chat and embedding, missing-key failure naming both variable names, `auth=sub` openai-only with the auth-file fixture — all before any call.
- R-BY7J-B037 — live smoke (`-tags eval_live`, real keys): one real chat analysis parses into a `QueryAnalysis` with ≥1 sub_query after normalization and one real embedding call returns a non-empty vector of the configured dimensions.
- Suite green (`go test ./...` from `wiki/`), including all pre-existing D66 ids still tagged and passing.
