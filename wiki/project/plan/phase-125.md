# Phase 125 — Testing-language conformance: move the three env-gated smokes into the live layer and declare the tree's testing facts

*Realizes design Decision 73 (testing-language conformance).*

Two test packages plus the tree's `AGENTS.md` and one new test file. Four
changes, all in `wiki/`:

1. **Move the `/embed` smoke behind the tag.** Cut
   `TestEmbedAgainstLivePrompts` (id `R-15NY-IF46`) out of
   `wiki/internal/llm/embed_test.go` into a new
   `wiki/internal/llm/embed_live_test.go` whose first line is `//go:build live`,
   same package `llm`. Its assertions are unchanged (one vector per input, each
   512-dimensional). Its `WIKI_LIVE_PROMPTS_URL` guard becomes a `t.Fatalf`
   naming the variable. Remove from `embed_test.go` any import (`os`) left
   unused.
2. **Move the two judge smokes behind the tag.** Cut
   `TestCompileScoreLiveJudgeReturnsComposedRubricScore` (`R-AFK7-HJ0T`) and
   `TestSynthesisScoreLiveJudgeReturnsComposedRubricScore` (`R-AGS3-VARI`) out
   of `wiki/autotune/folders_test.go` into a new
   `wiki/autotune/folders_live_test.go` whose first line is `//go:build live`,
   same package `autotune`. Their assertions are unchanged. The
   `WIKI_TUNE_LIVE != "1"` sentinel is **deleted, not converted** — the build
   tag is now the only boundary; the `OPENAI_API_KEY` check becomes a
   `t.Fatalf` naming the variable. The shared helpers (`runScorer`,
   `closeEnough`, `readJSON`) stay in the untagged `folders_test.go`, which
   compiles under both builds. Remove from `folders_test.go` any import left
   unused by the move.
3. **Declare the testing facts in `wiki/AGENTS.md`.** Its Tests section replaces
   the single `go test ./...` line with the declarations D73's table states: the
   default-gate command `go test ./...` (also `make test`, and that green means
   clean `go build ./...`, `go vet ./...`, `gofmt -l .` too); the layers present
   — **hermetic**, **composed**, **live**; that there is **no** environmental
   precondition beyond the Go toolchain; the **GOWORK mode** (workspace for the
   default gate, `GOWORK=off` forced by the production build); and the live
   invocation `go test -tags live ./...` with its `WIKI_LIVE_PROMPTS_URL` and
   `OPENAI_API_KEY` credentials, run at deploy verification.
4. **Add the two conformance tests** in a new hermetic file
   `wiki/cmd/wiki/docs_test.go` (the sibling-service idiom: shipped-file and
   doc-truth guards live in `cmd/<svc>/`), each tagged with its adopted id:
   - `R-O1AD-MRKW` — reads `../../AGENTS.md` from disk, isolates its `## Tests`
     section, and asserts that section names the default-gate command, each of
     the three layer names, the no-precondition statement, and the GOWORK mode.
     It must fail if any one of those is missing, and must not pass on a match
     found elsewhere in the file.
   - `R-O2IA-0JBL` — walks the tree for `*_test.go` files, skips any whose
     source carries the `live` build constraint, and asserts zero occurrences of
     `t.Skip`, `t.Skipf`, and `t.SkipNow`. The needle is assembled from parts at
     runtime (e.g. `"t." + "Skip"`) so the scan never matches its own source.
     Report every offending file and line.

**Done when:**

- `R-O1AD-MRKW` is covered by a test that reads the committed `AGENTS.md` and
  fails when a required declaration is absent.
- `R-O2IA-0JBL` is covered by the self-excluding source scan and passes with
  zero findings.
- `cd wiki && grep -rn 't\.Skip' --include='*_test.go' .` prints **nothing**.
- `cd wiki && grep -rn 'WIKI_TUNE_LIVE' --include='*.go' .` prints **nothing** —
  the sentinel is gone, not relocated.
- `cd wiki && grep -c 'go:build live' internal/llm/embed_live_test.go` and
  `cd wiki && grep -c 'go:build live' autotune/folders_live_test.go` each return
  `1`, and `cd wiki && go vet -tags live ./...` succeeds — the live layer
  compiles under its tag.
- `cd wiki && go test -run 'TestEmbedAgainstLivePrompts|LiveJudge' ./...`
  reports **no tests matched** in the default build — the three smokes are out
  of the gate.
- The suite is green: `cd wiki && go build ./...`, `go vet ./...`, `gofmt -l .`
  (no output), `go test ./...` all succeed with zero failures.
