# D11-runtime-argument-validation

Between accepting a tool set and dispatching a model's tool call, agentkit runs two
guards: a **one-time schema-and-name gate at `Send`**, and a **per-call argument
validation before dispatch**. Both are the library's, not the tool author's — a
tool's `Call` (D9) is invoked only with arguments already checked against its
schema, and a tool never has to defend itself against a malformed call.

**The `Send`-time gate runs once over the whole live tool set.** The live set is
the union of the eagerly registered tools, the deferred tool groups, and the
synthesized `load_tools` meta-tool (D16). The gate enforces two properties across
that union: **name uniqueness** — no two tools (across eager, deferred, and
`load_tools`) share a name, since the model addresses a tool only by name and a
collision makes dispatch ambiguous — and **canonical-subset validity** — every
tool's schema passes `ValidateToolSchema` (D9), including a schema that arrived via
`NewToolFromSchema` from a discovered source. A failure at this gate is a
configuration fault: `Send` returns `ErrInvalidConfig` before any provider call,
and `History` is unchanged (D2). This is the same fail-loud contract as an
unrepresentable reasoning shape (D8) and a reserved-key collision (D6).

```go
// validateToolSet is the Send-time gate over the live tool set (eager ∪ deferred
// groups ∪ load_tools). It checks name uniqueness and, via ValidateToolSchema,
// canonical-subset validity of every schema. A fault returns ErrInvalidConfig
// before any provider call. It is unexported — the orchestrator owns it; tool
// authors never call it (D17).
func validateToolSet(tools []Tool) error
```

**Per-call, three failure modes all resolve in-band.** When the model emits a
`ToolUse` (D2), the orchestrator resolves and dispatches it, and three things can
go wrong — none of them ends the turn:

- **Unknown tool name.** The model named a tool not in the live set (including a
  deferred-but-unloaded tool called directly, D16). agentkit does **not** guess an
  input or execute anything; it feeds back an `IsError` `ToolResult` naming the
  problem, and — for a deferred-but-unloaded tool — loads that tool as a side
  effect so the model can retry in the next round-trip.
- **Argument-validation failure.** The call's arguments fail validation against the
  tool's schema. agentkit does **not** call `Call`; it returns an `IsError`
  `ToolResult` describing the validation error, which the model can correct.
- **Tool returned an error.** `Call` ran and returned a non-nil error. That error
  becomes the `Content` of an `IsError` `ToolResult`.

In every case the result is an ordinary `ToolResult` with `IsError: true`, appended
to the transcript and sent back to the model on the next round-trip, exactly like a
successful result. A tool failure is a message the model can reason about and
recover from — it is never a `Stream.Err()` (D13) and never aborts the turn. The
turn ends only when the model stops requesting tools, or on a transport/protocol
error surfaced by the classifier (D4).

```go
// dispatch runs one resolved ToolUse and always yields a ToolResult. An unknown
// name, an arguments-vs-schema mismatch, and a Call that returns an error all
// produce an IsError result the model may recover from; none is turn-ending. The
// arguments are validated against the tool's schema here, so a Tool.Call sees
// only well-formed input.
func (o *orchestrator) dispatch(ctx context.Context, call ToolUse) ToolResult
```

Argument validation lives at the root, not in the tool. The old design exposed a
`validateToolArguments` helper for authors to call defensively; agentkit does the
validation before dispatch and does **not** export it (D17), so there is exactly
one validation path and a tool author writes only the tool's logic. Validation is
against the same canonical-subset schema the wire rendered (D10), so a call the
model could legally have been shown is a call the tool will legally receive.

## REQUIREMENTS

- R-4F8G-BWP5: `Send` MUST run a one-time gate over the live tool set (eager ∪ deferred groups ∪ `load_tools`) checking name uniqueness and canonical-subset schema validity, and MUST return `ErrInvalidConfig` before any provider call on failure, leaving `History` unchanged.
- R-4GGC-POFU: The name-uniqueness check MUST span eager tools, deferred tool groups, and the synthesized `load_tools` tool together, since the model addresses tools by name across the whole live set.
- R-4HO9-3G6J: A tool call's arguments MUST be validated against the tool's schema before dispatch, and `Tool.Call` MUST be invoked only with arguments that passed validation.
- R-4IW5-H7X8: A call to an unknown tool name MUST produce an `IsError` `ToolResult` without executing anything and MUST NOT end the turn; a deferred-but-unloaded tool named directly MUST additionally be loaded as a side effect.
- R-4K41-UZNX: An argument-validation failure MUST produce an `IsError` `ToolResult` describing the failure without calling the tool, and MUST NOT end the turn.
- R-4LBY-8REM: A tool whose `Call` returns an error MUST yield an `IsError` `ToolResult` carrying that error, and MUST NOT surface through `Stream.Err()` or abort the turn.
- R-4MJU-MJ5B: Runtime argument validation MUST live in the orchestrator and MUST NOT be exported for tool authors, so exactly one validation path exists.
