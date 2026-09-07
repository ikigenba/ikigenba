# D28-parallel-tool-dispatch

Every provider agentkit speaks lets the model request several tool calls in one
response, and every one of them does so by default. Until this design the
orchestrator ran those calls one after another. Now it runs them **at the same
time wherever that is safe**, and the tool author is the one who says what safe
means, because only the tool knows what it touches.

The whole consumer-facing surface is one extra argument to each tool
constructor and one field on `Settings`.

```go
package agentkit

// Access is what one tool call blocks. It is built only through the three
// constructors below; its fields are unexported and the scheduler is the only
// reader. Listed from least to most restrictive.
type Access struct{ /* unexported */ }

// BlocksNone: the call blocks no other call and is blocked by nothing but a
// BlocksAll call. A tool that only reads.
func BlocksNone() Access

// BlocksPaths: the call blocks any other call whose paths overlap these, for as
// long as it runs. Two paths overlap when, after filepath.Clean, one equals the
// other or is an ancestor directory of it. A tool that writes one file names
// that file; a tool that walks a tree names the tree's root. Zero paths is
// BlocksNone.
func BlocksPaths(paths ...string) Access

// BlocksAll: the call blocks every other call and waits for every call before
// it. A shell.
func BlocksAll() Access
```

The tool declares its access as a function of its input, beside the call
function, and both are required (D9):

```go
type writeInput struct {
	FilePath string `json:"file_path" jsonschema:"required"`
	Content  string `json:"content"   jsonschema:"required"`
}

func writeCall(ctx context.Context, in writeInput) (string, error) {
	return "ok", os.WriteFile(in.FilePath, []byte(in.Content), 0o644)
}

func writeAccess(in writeInput) agentkit.Access {
	return agentkit.BlocksPaths(in.FilePath)
}

write, err := agentkit.NewTool("Write", "Write a file", writeCall, writeAccess)
```

**Scheduling is by the model's call order.** When a round-trip requests
several calls, the orchestrator validates each call's arguments (D11), asks
each dispatchable call for its `Access` with those validated arguments, and
then starts calls in the order the model listed them, subject to one rule: a
call starts only after every earlier call it conflicts with has returned. A
call that conflicts with nothing ahead of it starts immediately, alongside
whatever is already running. Two calls conflict when either is `BlocksAll`, or
when both are `BlocksPaths` and a path of one overlaps a path of the other.
`BlocksNone` conflicts only with `BlocksAll`. The rule is simple enough to hold
in one's head, cannot deadlock (every call waits only on calls listed before
it), and is deterministic in what it admits, so a test can pin it.

A call that never reaches `Call` — an unknown name, an argument-validation
failure — is answered in-band (D11) and never consulted for access. The
synthesized `load_tools` meta-tool and the side-effect load of a
deferred-but-unloaded tool (D16) mutate the live tool set and are scheduled as
`BlocksAll`.

**What the consumer sees.** `ToolReturn` events (D13) and `tool_result` log
records (D15) are emitted as each call returns, so a renderer shows progress in
completion order; the `ToolUse` id on every result pairs it with its
`ToolCall`. The `RoleTool` message that carries the results back to the model
(D2, D15) lists them in the model's call order regardless of completion order,
so the transcript and every wire encoding stay stable. A round ends only when
every call has returned, cancellation included: cancelling the `Send` context
cancels every in-flight call, and the orchestrator waits for them before it
ends the turn, so no tool outlives its turn.

**The provider-side switch.** Independently of how agentkit runs the calls it
receives, a consumer may ask the model to request at most one call per
round-trip. `Settings.SerialToolCalls` is that ask. Its zero value leaves the
vendor default (parallel) in place, in keeping with D8's rule that a zero
`Settings` sends nothing. Each wire renders it in its own grammar when the
request advertises tools — Anthropic inside `tool_choice` as
`disable_parallel_tool_use`, the Chat and Responses families as the top-level
`parallel_tool_calls: false` — and the Gemini wire, whose grammar has no such
control, fails at `Send` with `ErrInvalidConfig` rather than silently ignoring
it (D8). Each vendor's acceptance of the rendered field is proved live (D23).

```go
package agentkit

type Settings struct {
	Options         Options
	ToolChoice      ToolChoice
	SerialToolCalls bool // ask the model for at most one tool call per round-trip
}
```

## REQUIREMENTS

- R-CZZ8-ZOQD: `agentkit` MUST export `Access` as an opaque struct type with no exported fields together with `func BlocksNone() Access`, `func BlocksPaths(paths ...string) Access`, and `func BlocksAll() Access`.
- R-D175-DGH2: Two tool calls MUST be treated as conflicting exactly when either call's `Access` is `BlocksAll()`, or both are `BlocksPaths` and some path of one, after `filepath.Clean`, equals or is an ancestor directory of some path of the other; `BlocksNone()` and `BlocksPaths()` with no paths MUST conflict with nothing but a `BlocksAll()` call.
- R-D2F1-R87R: For each tool call of a round-trip that passes argument validation (D11), the orchestrator MUST invoke the tool's `Access` exactly once with the validated arguments before starting the call, and MUST NOT invoke `Access` for a call answered without dispatch (an unknown name or an argument-validation failure).
- R-D3MY-4ZYG: Within one round-trip the orchestrator MUST start each tool call only after every earlier call, in the order of the assistant message's `ToolUse` blocks, that conflicts with it has returned, and MUST NOT delay a call on any call it does not conflict with; verified by a round of non-conflicting calls that each block until every one of them has started, which MUST complete, and by a round of conflicting calls whose executions MUST never overlap in time.
- R-D4UU-IRP5: The orchestrator MUST yield each `ToolReturn` event and write each `tool_result` record as its call returns, without waiting for any other call of the round-trip, so that a fast call's `ToolReturn` is observed before a concurrent slow call returns.
- R-D62Q-WJFU: The orchestrator MUST NOT write the round-trip's `RoleTool` message, make the next provider call, or end the turn while any tool call of the round-trip is still running, including when the `Send` context is cancelled; every `Call` of a round-trip MUST receive a context that is cancelled when the `Send` context is cancelled.
- R-D7AN-AB6J: A call to the synthesized `load_tools` meta-tool and a direct call to a deferred-but-unloaded tool (D16) MUST be scheduled as `BlocksAll()` calls.
- R-D8IJ-O2X8: When `Settings.SerialToolCalls` is `true` and the request advertises at least one tool, `AnthropicMessagesWire()` MUST render a `tool_choice` object carrying `"disable_parallel_tool_use":true`, with `"type":"auto"` when `ToolChoice.Mode` is `ToolChoiceAuto` and the D8 rendering of the mode otherwise, pinned by a golden fixture.
- R-D9QG-1UNX: When `Settings.SerialToolCalls` is `true` and the request advertises at least one tool, `ChatWire()`, `OpenAIChatWire()`, `XAIChatWire()`, `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()` MUST each render the top-level field `"parallel_tool_calls":false`, pinned by a golden fixture.
- R-DAYC-FMEM: A `Send` on a conversation whose wire is `GeminiGenerateContentWire()` with `Settings.SerialToolCalls` `true` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged.
- R-DDE5-75W0: When `Settings.SerialToolCalls` is `false`, or when the request advertises no tool, no shipped wire MUST emit `disable_parallel_tool_use` or `parallel_tool_calls` in the request body.
- R-DEM1-KXMP: The module MUST contain the file `serial_tool_calls_live_test.go`, beginning with the build constraint `//go:build live`, containing a test named `TestLiveSerialToolCalls` that, for every R-WVYS-QG8U cell except `gemini-generate-content`, runs a tool turn with `Settings.SerialToolCalls` `true` on a conversation advertising two tools and asserts that `Stream.Err()` is nil and that no assistant `MessageDone` carries more than one `ToolUse` block.
- R-DFTX-YPDE: The `Access` returned by a tool built with `NewTool`, `MustTool`, or `NewToolFromSchema` MUST be the value the constructor's `access` argument returns for the call's validated arguments, decoded into `In` for the generic constructors and passed raw for `NewToolFromSchema`.
