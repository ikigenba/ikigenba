# D19-wire-tool-call-and-model-conformance

Two wire-conformance gaps sit between the design and a working tool loop, and
this document pins them with fresh ids so the build loop sees them. Both are
implied by earlier requirements whose ids already carry passing tests — D12's
"alternate round-trips and tool dispatch" presumes a decoder that emits tool
calls, and D1's "model transmitted verbatim" presumes an encoder that transmits
it — but the gap is computed from id presence alone, so an implied obligation
with no id of its own is invisible to the loop.

**Decode side: tool calls.** Every shipped wire's `DecodeStream` today assembles
an assistant `MessageDone` carrying only `Text`. The encode side already renders
`ToolUse` and `ToolResult` in each wire's grammar; the decode side must emit the
`ToolUse` block the orchestrator dispatches on. Each vendor carries a tool call
differently — a content block with an object `input`, a function-call item with
a JSON-*string* `arguments`, a delta-accumulated `tool_calls` array, a
`functionCall` part — and the wire owns that translation (D5). What reaches the
orchestrator is one shape: a `ToolUse` with the vendor's verbatim call id, the
tool name, and `Input` normalized to a JSON object regardless of whether the wire
carried it as a string or an object (D2). Text that arrives alongside a tool call
in the same assistant message is preserved, in order, as a sibling `Text` block.

**Encode side: the model.** No wire request body carries the model string, and no
endpoint places it in the path. Three wires — Anthropic Messages, OpenAI Responses,
OpenAI Chat — take the model as a top-level body field; the Gemini grammar takes
it in the URL path, which the endpoint owns (D6). The `gemini` vendor package
builds that path. The three body-grammar wires read the conversation's model
string and emit it; the Gemini wire emits none.

Vendor byte facts — field names, event types, the exact SSE shape of a tool call —
live in golden fixtures under `testdata/` and in the per-wire conformance tests,
not in requirement text (D5). The requirements below fix what the orchestrator may
rely on.

## REQUIREMENTS

- R-T44G-AS0I: The Anthropic Messages wire's `DecodeStream` MUST emit a `ToolUse` block for each tool call in the vendor stream, carrying the vendor's verbatim call id, the tool name, and the call input as a JSON object, pinned by a golden fixture under `testdata/`.
- R-T5CC-OJR7: The OpenAI Responses wire's `DecodeStream` MUST emit a `ToolUse` block for each function call in the vendor stream, carrying the vendor's verbatim call id, the function name, and the arguments as a JSON object, pinned by a golden fixture under `testdata/`.
- R-T6K9-2BHW: The OpenAI Chat wire's `DecodeStream` MUST emit a `ToolUse` block for each tool call in the vendor stream, assembling delta-streamed arguments into one JSON object and carrying the vendor's verbatim call id and function name, pinned by a golden fixture under `testdata/`.
- R-T7S5-G38L: The Gemini wire's `DecodeStream` MUST emit a `ToolUse` block for each function-call part in the vendor stream, carrying the call id the wire correlates on, the function name, and the arguments as a JSON object, pinned by a golden fixture under `testdata/`.
- R-T901-TUZA: A `ToolUse.Input` emitted by any shipped wire MUST be a JSON object (never a JSON-encoded string), so the orchestrator validates and dispatches one argument encoding.
- R-TA7Y-7MPZ: When an assistant message carries both text and tool calls, every shipped wire MUST emit the `Text` and `ToolUse` blocks in the vendor's order within one `MessageDone`, dropping neither.
- R-ORPQ-5I48: The Anthropic Messages wire MUST emit the conversation's model string verbatim as the top-level `model` field of the request body, pinned by the wire's request fixture.
- R-OU5I-X1LM: The OpenAI Responses and OpenAI Chat wires MUST each emit the conversation's model string verbatim as the top-level `model` field of the request body, pinned by each wire's request fixture.
- R-TDVN-CXY2: The Gemini wire's `EncodeRequest` MUST NOT emit a model field in the request body, and the `gemini` vendor constructor MUST place the model in the request URL path.
- R-TF3J-QPOR: An offline turn driven against a replayed tool-call fixture MUST complete the full loop — `ToolCall` emitted, the tool dispatched, `ToolReturn` emitted, a second round-trip whose request body carries the `ToolUse` and `ToolResult`, and a final `MessageDone` — for every shipped wire.
