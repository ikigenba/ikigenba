# Phase 100 — The eval runner (`cmd/eval-extract`) over agentkit

*Realizes design Decision 66 (all ids). Depends on Phase 99.*

The workbench's measure step becomes runnable:

- `internal/extract` exports the two production-path seams `Render` and
  `Validate` (thin over the existing internals — the production `Extract` path
  itself calls them, so they cannot drift from it).
- `wiki/go.mod` gains `github.com/ikigenba/agentkit v0.7.0`.
- `cmd/eval-extract` — the `run` and `compare` verbs per D66: agentkit
  anthropic chat (with production-shaped parse/validate retries) and openai
  embeddings behind the D65 `EmbedFunc` + disk cache; config/flag handling with
  the effective call config echoed in the scorecard; atomic scorecard writes;
  loud failure exits; `-repeat`/epsilon; `compare`'s accept/reject exit codes.
- The live smoke test, build-tagged `eval_live`, excluded from the default
  suite.

Phase 97's confinement tests (agentkit only under `cmd/eval-extract/` +
`internal/eval/`, keys only in `cmd/eval-extract/`) must still pass — they are
the boundary this phase builds inside.

**Done when:**
- R-KY7O-D7JB — envelope identity: production's sent prompt equals `Render(...)`; production-rejected responses are `Validate`-rejected — covered by a tagged test.
- R-KZFK-QZA0 — `run` end-to-end with fakes: scores the dev split, writes the scorecard, exits 0; a bad-then-good response is re-prompted — covered by a tagged test.
- R-L0NH-4R0P — flag overrides applied and echoed verbatim in the scorecard — covered by a tagged test.
- R-L1VD-IIRE — unreadable prompt / malformed gold / retries-exhausted call each exit non-zero naming the cause, with no scorecard left behind — covered by a tagged test.
- R-L339-WAI3 — `-repeat 3` records three composites and epsilon = max−min — covered by a tagged test.
- R-L4B6-A28S — `compare` exits 0 strictly above best+epsilon, 1 otherwise — covered by a tagged test.
- R-L5J2-NTZH — the live smoke exists under the `eval_live` build tag (real chat extraction validates with ≥1 subject; real embed returns configured-dimension vectors); `go vet -tags eval_live ./cmd/eval-extract/...` is clean and the default suite (untagged) does not run it.
- The suite is green per design Conventions.
