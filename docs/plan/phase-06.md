# Phase 6 — Module swap, provider factory, and runner rewrite

*Realizes design Decision 1 (Module dependency), Decision 4 (Provider factory), and Decision 7 (Runner). Depends on Phase 01, Phase 04, Phase 05.*

This is the final phase. All remaining local agentkit imports are removed.

**`prompts/go.mod`**: remove the `require agentkit v0.0.0` line and its `replace agentkit => ../agentkit` directive. The `require github.com/ikigenba/agentkit v0.1.0` added in Phase 01 is the sole agentkit dependency from this point. The `appkit` and `eventplane` replace directives are untouched.

**`prompts/internal/runner/runner.go`**: remove all imports of `agentkit/agent`, `agentkit/model`, `agentkit/provider`, `agentkit/provider/anthropic`, `agentkit/tools`, `agentkit/wire`. Add `buildProvider` as a package-level function and an injectable field on `Runner` (D4): it switches on `cfg.Provider`, reads the API key via `getenv`, constructs the matching published provider, and for `"zai"` passes `WithBaseURL` when `cfg.BaseURL` is set. Change the `discover` field type from `func(...) agent.ToolSource` to `func(...) []agentkit.Tool` (D6). Rewrite `execute` to use `agentkit.Conversation.Send` + stream drain (D7): build provider, build conversation with `tools.All` + discover result appended, call `conv.Send`, drain events, read `stream.Err()` / `stream.Usage()` / `stream.Cost()`, call `conv.Close()`. Replace `captureUsage` (file scanner) with `serializeUsage` (direct marshal of `agentkit.Usage` and `agentkit.Cost`). Copy `FramingPrompt` from the local agentkit into this package. Remove `clientFactory`, `defaultClientFactory`, `buildRequest`, `wire.NewSession`, `wire.Session`, and all references to `model.Resolve`, `model.ModelContext`, `model.DefaultEffort`. Tests inject a `fakeProvider` (implements `Name`, `Pricing`, `RoundTrip`) that completes in one turn with a canned text response.

**Done when:** R-K5I9-YGS9, R-K6Q6-C8IY, R-K7Y2-Q09N, and R-K95Z-3S0C are each covered by a clearly-named test and `go test ./...` is green.
