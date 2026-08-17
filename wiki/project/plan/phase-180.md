# Phase 180 — Restore strict-tier lint cleanliness

*Realizes design Decision 99 (adopt the suite lint contract at strict).*

Decision 99 registers this tree at lint tier `strict` and obliges it to be
**clean at that tier** (`bin/lint wiki` exits 0). The tree has since regressed:
feature work reintroduced complexity, nesting, length, and unnamed-result
findings, so the strict gate — the one `bin/ship` enforces — now fails and the
service cannot ship. This phase brings the tree back to a clean strict run.

All work is internal and changes no exported signature, type, or seam (D99:
these findings are "fixable without changing any exported signature or seam"),
and no observable service behavior changes. The current findings, by analyzer:

- **gocyclo (13)** — reduce cyclomatic complexity below the strict threshold
  (> 15) by extracting cohesive helpers from the flagged functions:
  `internal/ask/ask.go` (`(*Asker).Ask`), `internal/llmtest/client.go`
  (`serve`), `internal/mcp/mcp.go` (`Tools`),
  `internal/wiki/completion_queue.go` (`applyCompletion`, `applyExtract`,
  `stageExtractPlan`, `applyMatch`, `applyCompile`, `integrateStagedTx`),
  `internal/wiki/links.go` (`markdownSkipRegions`), and
  `internal/wiki/service.go` (`integrate`, `mergeSubjects`,
  `mergeEffectiveClaims`).
- **nestif (2)** — flatten the complex nested blocks in
  `internal/wiki/completion_queue.go` and `internal/wiki/links.go` (early
  returns / guard clauses / helper extraction).
- **funlen (1)** — shorten `newSpec` in `cmd/wiki/main.go` (156 > 80) by
  factoring its construction into helpers.
- **gocritic unnamedResult (15)** — name the multiple return results on the
  flagged functions in `internal/mcp/mcp.go`, `internal/wiki/data_model.go`,
  `internal/wiki/service.go`, `internal/wiki/completion_queue.go`,
  `internal/wiki/config.go`, and `internal/llmtest/client.go`.

Refactoring only; the hermetic, composed, and live test layers must stay green
and prove behavior is unchanged. No `.lint-tier` change (it already says
`strict`) and no new dependencies.

**Done when:** all of the following hold, from a clean checkout:

- `bin/lint wiki` (run from the repo root; read-only over this tree) exits `0`
  and reports `findings=0`.
- `cd wiki && go build ./...`, `go vet ./...`, and `gofmt -l .` (no output) all
  succeed.
- `cd wiki && go test ./...` is green (zero failures).

Structural phase — realizes no Verification ids; its proof is the deterministic
exit conditions above (D99 mints none, and the lint contract carries no
per-service ids).
