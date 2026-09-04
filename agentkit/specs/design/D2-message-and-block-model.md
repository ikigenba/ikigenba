# D2-message-and-block-model

A conversation's transcript is a `History` — an ordered slice of `Message`, each a
role plus a slice of `Block`. A `Block` is the atom of content: a piece of text, a
span of model reasoning, a tool call, or a tool result. `Block` is a **sealed
union** — an interface with an unexported marker method so no package outside
agentkit can add a variant, which is what lets the orchestrator and every wire
codec switch over the closed set exhaustively.

A `History` is a slice of `Message`, and a `Message` is a role paired with a
`Block` slice — the assistant's turn is one `Message` whose blocks carry its text
and reasoning in order, a tool round-trip is a `Message` of `ToolResult` blocks,
and so on. `Role` is a small closed enumeration; system context is a role, not a
side channel, so a wire that carries system text out-of-band (in a top-level
field) renders it from the `RoleSystem` message rather than the library keeping a
separate slot.

```go
package agentkit

// Role names who authored a Message. It is a closed enumeration; a wire that
// carries one role out-of-band (e.g. system text in a top-level field) renders
// it from the corresponding Message, so the transcript stays uniform.
type Role int

const (
	RoleSystem    Role = iota // system / developer context
	RoleUser                  // the consumer's input
	RoleAssistant             // the model's output
	RoleTool                  // tool results fed back to the model
)

// Message is one entry in a History: a role and the blocks it authored, in
// order. History is a []Message and always ends at a turn boundary (D12).
type Message struct {
	Role   Role
	Blocks []Block
}

// Block is one atom of message content: text, reasoning, a tool call, or a tool
// result. It is a sealed union — the unexported isBlock marker keeps the variant
// set closed so wire codecs and the orchestrator can switch it exhaustively.
type Block interface {
	// BlockType returns the serialization discriminator ("text", "reasoning",
	// "tool_use", "tool_result"). It exists so a History round-trips through
	// JSON: a sealed interface slice cannot unmarshal without a tag naming the
	// concrete type. This is a *serialization* concern only — it is NOT an
	// endpoint tag and NOT a replay discriminator (see Provider payload below).
	BlockType() string
	isBlock()
}
```

Every variant carries an **opaque provider payload** — bare bytes the wire codec
emitted on parse and replays byte-identical on the next request. This is seam 1,
and it lives on *every* variant, not just reasoning: some wires attach a
signature to visible-text and tool-call content, so the payload cannot be a
reasoning-only field. The payload is bare bytes with **no endpoint tag and no
`kind` discriminator**. It needs none: provider and model are immutable for a
conversation's life (D1), so every block in a given History came from one wire,
and nothing ever has to ask "which wire produced this block?". The block's own Go
type tells the adapter where its payload belongs; the rules for interpreting a
payload live in the wire's parse/build steps, never in the payload itself. This
bare-bytes payload and the `BlockType()` discriminator are different concerns: the
discriminator is for JSON serialization of the sealed slice; the payload is opaque
replay material the library never inspects.

```go
package agentkit

// Text is spoken content — a user prompt or a model's visible reply.
type Text struct {
	// Text is the content, UTF-8, present even when empty.
	Text string
	// Provider is opaque replay material the wire emitted for this block; the
	// library never inspects it and replays it verbatim on the next request.
	Provider json.RawMessage
}

// Reasoning is a span of model thinking. Whether a block is reasoning is decided
// by the wire's parse step (e.g. from a "thought" flag), never by the presence
// of Provider bytes.
type Reasoning struct {
	// Text is the reasoning content the wire chose to expose; may be empty.
	Text string
	// Redacted marks reasoning the vendor returned in encrypted/redacted form,
	// which still must be replayed in order and unfiltered.
	Redacted bool
	// Provider carries the signature / encrypted-content bytes replayed verbatim.
	Provider json.RawMessage
}

// ToolUse is the model's request to call a tool.
type ToolUse struct {
	// ID is the vendor's own call id, carried verbatim — agentkit mints no
	// neutral id and correlates ToolResult to ToolUse on this exact string.
	ID string
	// Name is the tool the model chose.
	Name string
	// Input is the decoded argument object (agentkit normalizes the wire's
	// string-vs-object argument encoding into RawMessage at parse time).
	Input json.RawMessage
	// Provider carries any per-call replay bytes (e.g. a part-level signature).
	Provider json.RawMessage
}

// ToolResult is the outcome of running a tool, fed back to the model.
type ToolResult struct {
	// ToolUseID matches the ToolUse.ID this result answers.
	ToolUseID string
	// Content is the tool's textual output.
	Content string
	// IsError marks an in-band tool failure the model may recover from; it is
	// never turn-ending (D12).
	IsError bool
	// Provider carries any replay bytes the wire attaches to a result block.
	Provider json.RawMessage
}
```

`History` is **JSON-round-trippable**: it marshals to a stable shape and
unmarshals back to the same sequence of concrete `Block` variants, keyed by the
`BlockType()` discriminator. Its structural invariant is that it **always ends at a
turn boundary** — a whole assistant turn's blocks are buffered during `Send` and
appended once, on success (D12). A turn that fails mid-loop appends nothing, so a
serialized History never captures a half-turn. The consumer has already observed
every completed round-trip through the event stream, so nothing is lost by not
persisting a partial turn.

## REQUIREMENTS

- R-1Y7S-ENKG: `Block` MUST be a sealed union — an interface with an unexported marker method — so no package outside agentkit can introduce a variant, and every wire codec MUST switch it exhaustively.
- R-1ZFO-SFB5: Every `Block` variant MUST carry an opaque provider payload as bare bytes with no endpoint tag and no `kind` discriminator, and agentkit MUST replay that payload byte-identically without inspecting it.
- R-20NL-671U: Each `Block` variant MUST report a stable serialization discriminator via `BlockType()`, distinct from the opaque provider payload, sufficient for a `History` to unmarshal back to the correct concrete variants.
- R-21VH-JYSJ: A `History` MUST round-trip through `json.Marshal`/`json.Unmarshal` to an equal sequence of concrete `Block` variants, including each block's provider payload.
- R-U2LH-88H7: When a wire decodes a vendor tool call, the resulting `ToolUse.ID` MUST equal the vendor's call id verbatim (including surrounding whitespace or punctuation), and re-encoding a `ToolResult` whose `ToolUseID` is that value MUST place the same id on the wire unchanged; agentkit MUST NOT generate or substitute its own identifier at any step.
- R-25J6-PA0M: A `History` MUST always end at a turn boundary — the blocks of a turn that fails mid-loop MUST NOT be appended, so no serialized History contains a partial turn.
- R-YX7D-BDFM: `agentkit` MUST export `type Role int` with the constants `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool` declared in that `iota` order starting at 0.
- R-YYF9-P56B: `agentkit` MUST export `type Message struct { Role Role; Blocks []Block }` with exactly those two fields.
- R-YZN6-2WX0: `agentkit` MUST export `type History []Message` as a named slice type.
- R-Z0V2-GONP: `agentkit` MUST export `type Text struct { Text string; Provider json.RawMessage }`, and `Text` MUST implement `Block`.
- R-Z22Y-UGEE: `agentkit` MUST export `type Reasoning struct { Text string; Redacted bool; Provider json.RawMessage }`, and `Reasoning` MUST implement `Block`.
- R-Z3AV-8853: `agentkit` MUST export `type ToolUse struct { ID string; Name string; Input json.RawMessage; Provider json.RawMessage }`, and `ToolUse` MUST implement `Block`.
- R-Z4IR-LZVS: `agentkit` MUST export `type ToolResult struct { ToolUseID string; Content string; IsError bool; Provider json.RawMessage }`, and `ToolResult` MUST implement `Block`.
