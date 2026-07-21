# Phase 97 — Re-scope the retirement proofs: agentkit confined, not banned

*Realizes design Decision 1 (prompts dependency; agentkit confined to the dev workbench) and Decision 63 (the retirement's current scope).*

The retirement tests still assert the pre-workbench world (eval tree absent,
agentkit absent everywhere, no agentkit require in go.mod) under the retired ids
`R-0UOV-2HFX`, `R-0VWR-G96M`, `R-1E79-6TB1`, `R-1GN1-YCSF`. This phase rewrites
those proofs to the current design before any workbench code lands, so later
phases don't break the suite by existing:

- `cmd/wiki/module_wiring_test.go` and `internal/wiki/retirement_test.go` (and
  any other test carrying the four retired ids) are rewritten to assert the new
  boundary: the production binary's dependency closure is agentkit-free
  (R-KDHD-V3XI), provider-key reads are confined to `cmd/eval-extract/`
  (R-KEPA-8VO7), the recorder-stack identifiers stay gone while
  `extract.DefaultPromptInstructions` stays exported (R-KFX6-MNEW), and agentkit
  imports are confined to `cmd/eval-extract/` + `internal/eval/` (R-KH53-0F5L).
- The four retired ids disappear from the test tree with the assertions they
  tagged. No new packages, no new dependencies in this phase — the new
  assertions must pass both now (vacuously, where the eval tree doesn't exist
  yet) and after phases 98–100 land.

**Done when:**
- R-KDHD-V3XI — with `GOWORK=off`, `go list -deps ./cmd/wiki` succeeds with no `ikigenba/agentkit` package — covered by a tagged test.
- R-KEPA-8VO7 — `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` appear in no non-test source outside `cmd/eval-extract/` — covered by a tagged test.
- R-KFX6-MNEW — recorder-stack identifiers absent from non-test source; `extract.DefaultPromptInstructions` exported — covered by a tagged test.
- R-KH53-0F5L — agentkit imports confined to `cmd/eval-extract/` + `internal/eval/` — covered by a tagged test.
- `grep -rnE 'R-0UOV-2HFX|R-0VWR-G96M|R-1E79-6TB1|R-1GN1-YCSF' --include='*_test.go' .` from `wiki/` returns nothing.
- The suite is green per design Conventions.
