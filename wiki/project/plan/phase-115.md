# Phase 115 — Remove the tuning machinery

*Realizes design Decision 70 (removal) and the retirement ids of Decisions 1 and 63. Depends on Phase 112, 113 (prompts rehoused) and 114 (analysis gold converted before its source tree is deleted).*

Delete `cmd/autotune/`, `cmd/eval-extract/`, `cmd/eval-analysis/`, `internal/eval/`, and the whole `eval/` tree (tracked files via git, plus any untracked residue on disk). Remove the Makefile `eval-extract` build line; drop the `github.com/ikigenba/agentkit` require from `go.mod` (`go mod tidy`); delete `internal/llm`'s deprecated compatibility fields (`CallSite.Model/Temperature/Reasoning/MaxTokens`, `DisableReasoning`) and any lingering references. Rewire `.envrc` to export `OPENAI_API_KEY` (replacing `EVAL_OPENAI_API_KEY`); update `.gitignore` (drop `/autotune/`, keep `/tmp/`); true up `AGENTS.md`'s layout section (no `eval-extract`, no `internal/eval`, no `eval/`; name `autotune/` as the committed tune-folder workspace). Delete the tests tagged with the retired ids R-KEPA-8VO7 and R-KH53-0F5L and realize their replacements. `internal/llmtest` stays.

**Done when:**
- R-A3D7-NTLV — `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` appear in no non-test `.go` file in the module.
- R-A4L4-1LCK — no `.go` file (excluding `project/`) matches `ikigenba/agentkit`, and `go.mod` carries no agentkit require.
- `grep -rn "wiki/eval\|internal/eval" --include='*.go' .` (excluding `project/`) from `wiki/` returns nothing; the directories `cmd/autotune`, `cmd/eval-extract`, `cmd/eval-analysis`, `internal/eval`, and `eval` do not exist.
- The retired ids R-KEPA-8VO7 and R-KH53-0F5L appear in no test file; R-KDHD-V3XI and R-KFX6-MNEW remain green.
- The suite is green (design Conventions) with `GOWORK=off` builds still succeeding.
