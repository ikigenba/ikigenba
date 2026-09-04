# D12-turn-and-orchestration

The orchestrator is the loop behind `Send`. One `Send` drives one **turn**: it
appends the caller's blocks to the transcript, calls the model, and runs tool
round-trips to completion, returning a `*Stream` (D13) of message-granular events.
A turn is any number of provider round-trips — the model answers, and while its
answer is a set of tool calls the orchestrator dispatches them, feeds the results
back, and calls again, until the model returns a round-trip with no tool calls.
Wire, endpoint, and model are fixed for the conversation's life; only the
transcript grows. There is no swap-vendors machinery and no drop-reasoning-on-
switch rule, because a switch cannot happen.

A `Conversation` cleanly splits **config** (immutable, set at construction) from
**transcript** (grows one whole turn at a time). Config is the wire codec (D5) paired with
the `Endpoint` (D6), the `Model` string, the generation `Settings` (D8) — the
user's string options, the tool-choice directive, and the reasoning request —
the registered tool set (eager and deferred, D16), the optional structured-output
contract (D20), and the optional event `Log` (D15, which carries its own injected
clock) — all supplied through `Config` (D18). The transcript is the
`History` (D2). Nothing reassigns config after construction.

```go
package agentkit

// requestState is the immutable input the wire consumes for one round-trip
// (D5 EncodeRequest). It is unexported: only built-in wires read it. It is a snapshot: the History and Tools
// reflect this round-trip only — History grows across round-trips as tool results
// are appended, and Tools grows when load_tools runs (D16) — while Model and
// Settings are fixed for the conversation.
type requestState struct {
	Model    string          // verbatim model string (D1)
	History  []Message       // transcript snapshot for this round-trip
	Settings Settings        // options, tool choice, and reasoning shape (D8), already validated
	Tools    []Tool          // resolved live tool set for this round-trip (D9, D16)
	Output   *OutputContract // structured-output contract, nil for none (D20)
}
```

There is no raw pass-through map. An earlier revision carried `ProviderOptions`,
a `map[string]json.RawMessage` merged shallowly into the request body with only a
reserved-key collision check and every other key forwarded silently. It is gone:
every option a consumer can send is a string in `Settings.Options` that the wire
must recognize and parse (D8), so an unknown key is a `Send`-time
`ErrInvalidConfig`, never bytes the vendor sees.

**History is buffered, then appended once on success.** The orchestrator collects
every block the turn produces — assistant messages, tool-use blocks, the tool
results it runs — in a turn-local buffer, and splices that buffer onto `History`
only when the turn completes without a terminal error. A turn that fails
mid-loop appends **nothing**: `History` always ends at a turn boundary (D2), never
mid-turn. There are no eager writes and no rollback-by-truncation — the failed
turn simply never touched `History`. Nothing is lost, because the consumer already
observed every completed round-trip through the `Stream` as it happened.

**Tool errors are in-band, never turn-ending.** A tool returning an error becomes
a `ToolResult` with `IsError` set (D2), fed back to the model to recover from; the
same holds for an unknown tool name and for argument-validation failure (D11). A
terminal error — a transport failure, a classified vendor error (D4), an
unrecoverable decode — ends the turn, appends nothing, and surfaces on
`Stream.Err()` (D13). Tool-call ids are the vendor's own, verbatim: the
orchestrator correlates a `ToolResult` to its `ToolUse` on the exact id the wire
parsed (D2), minting no neutral id.

Config checks fail loud at the boundary rather than reaching the vendor.
**Settings** (D8): before the first round-trip, `Send` asks the wire to validate
`Settings` — every option key must be in the wire's vocabulary and parse under
its declared kind, and the reasoning and tool-choice shapes must be ones the
wire can express; any failure returns `ErrInvalidConfig` (D4) with no provider
call and `History` unchanged. **Credential mode** (L2): a credential whose mode
the catalog offering does not list is `ErrInvalidConfig` from
`Offering.Authenticator`, before any turn (D7). Both are of a piece with the
library's fail-loud stance (D4, D8, D9): a request the seam cannot faithfully
express is refused, not silently reshaped.

## REQUIREMENTS

- R-4NRR-0AW0: `Send` MUST drive a turn to completion — appending the caller's blocks, then alternating provider round-trips and tool dispatch until a round-trip yields no tool call — and MUST return a `*Stream` (D13) of message-granular events.
- R-OJ6F-H3XD: A `Conversation` MUST fix its wire codec, `Endpoint`, `Model`, `Settings`, and registered tool set at construction and expose no method to reassign them.
- R-4Q7J-RUDE: The orchestrator MUST buffer a turn's blocks and splice them onto `History` exactly once, only on successful completion; a turn that ends in a terminal error MUST leave `History` byte-for-byte unchanged.
- R-4RFG-5M43: A tool returning an error, an unknown tool name, and an argument-validation failure (D11) MUST each become a `ToolResult` with `IsError` set and be fed back to the model, and MUST NOT end the turn.
- R-4SNC-JDUS: A terminal error (transport, classified vendor error, unrecoverable decode) MUST end the turn, append nothing to `History`, and surface on `Stream.Err()`.
- R-4TV8-X5LH: The orchestrator MUST correlate each `ToolResult` to its `ToolUse` by the vendor's verbatim call id and MUST NOT substitute a library-minted identifier.
- R-NZTR-FBVP: `agentkit` MUST NOT export `ProviderOptions`, and `Conversation` MUST accept no raw JSON pass-through at construction or at `Send`.
