# Phase 5 — Suite discovery rewrite

*Realizes design Decision 6 (Suite discovery). Depends on Phase 04.*

**`prompts/internal/suite/suite.go`**: change `Discover`'s return type from `agent.ToolSource` to `[]agentkit.Tool`. The best-effort, never-crash contract is unchanged — unreachable peers are logged and skipped; `Discover` never returns an error and always returns a non-nil slice. Remove the `source` struct and its `Descriptors`, `Owns`, and `Dispatch` methods; replace the three internal maps with a single `[]agentkit.Tool` slice built during discovery. Wrap each discovered tool as an `agentkit.RawTool` using the qualified name, discovered description, discovered schema, and a dispatch closure that calls the peer and maps both transport errors and `isError` MCP responses to non-terminal Go errors.

The runner's `discover` field still has the old type until Phase 06; this package compiles independently of the runner.

**Done when:** R-K32H-6XAV and R-K4AD-KP1K are each covered by a clearly-named test and `go test ./...` is green.
