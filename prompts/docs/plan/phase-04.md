# Phase 4 — Built-in tools package

*Realizes design Decision 5 (Built-in tools). Depends on Phase 01.*

**`prompts/internal/tools/`**: new package. Implement `All(sandboxRoot string) []agentkit.Tool` returning six `agentkit.RawTool` values — bash, read, write, edit, glob, grep — each with its name, a short description (added for the first time; see D5 for the six descriptions), the existing JSON Schema, and a closure over `sandboxRoot` that calls the existing tool dispatch logic copied from the local agentkit packages. Sandbox confinement (path validation against sandboxRoot) is preserved unchanged inside each tool's call function. No package-level state; `All` is called once per run.

The local agentkit's `agentkit/tools` package is not yet removed — the runner still imports it (until Phase 06).

**Done when:** R-K0MO-FDTH and R-K1UK-T5K6 are each covered by a clearly-named test and `go test ./...` is green.
