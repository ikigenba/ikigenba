# D5-wire-codec-seam

A **`WireFormat`** is the codec for one vendor's HTTP body grammar and streaming
event vocabulary. It is internal: a `WireFormat` value is never an assignable
field on a `Conversation` and never appears in consumer code. A vendor
constructor (or the generic wire constructor) selects one and hands it, paired
with an `Endpoint` (D6), to the orchestrator. Four wires ship day one — Anthropic
Messages, OpenAI Responses, OpenAI Chat Completions, Gemini generateContent — and
a fifth day-one endpoint (xAI) rides the OpenAI Responses wire unchanged, which is
the whole reason wire and endpoint are separate axes (D6).

**What the wire owns**, and nothing else touches: the request body shape; the
streaming framing choice and its event vocabulary; where usage sits in the
response, its field names, and its subset topology (which token buckets nest
inside which — cached ⊂ input, reasoning ⊂ output, and so on); the tool
declaration shape and the tool-result shape; whether tool-call arguments travel
as a JSON string or a JSON object; reasoning replay in full — both its mechanics (how a
prior reasoning block is echoed back on the next turn) and its body encoding — and
whether a requested reasoning shape is even expressible on this wire. Everything
that is transport rather than grammar — base URL, auth, headers, error envelope —
belongs to the `Endpoint` (D6). The dividing question is "does this change the
bytes of the body grammar, or only where/how the body is sent?"

The codec is one interface. Encoding assembles a request body from the turn
state; decoding assembles message-granular protocol events from framed chunks and
terminates SSE (or other framing) decode here — no framing detail escapes into
the orchestrator. Usage merges field-wise, each field absolute, last-non-absent
wins (never whole-object last-wins), which is a decode-side invariant of the wire.

```go
// WireFormat is the internal codec for one vendor body grammar. It is selected
// by a constructor, never assigned by a consumer.
type WireFormat interface {
	// EncodeRequest renders the assembled turn state into a request body for
	// this wire. Model placement, headers, and base URL are the Endpoint's
	// concern (D6); EncodeRequest produces body bytes only.
	EncodeRequest(state RequestState) ([]byte, error)

	// DecodeStream consumes raw payload frames (from a Framer) and yields
	// assembled, message-granular events. It terminates framing decode here,
	// merges Usage field-wise (each field absolute, last-non-absent wins), and
	// surfaces an in-band vendor error frame through the error side of the seq.
	DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error]

	// RenderTools renders the canonical tool schemas (D9) into this wire's tool
	// declaration shape. A schema outside the canonical subset fails here,
	// before send.
	RenderTools(tools []Tool) (json.RawMessage, error)

	// ReservedKeys lists the ProviderOptions keys this wire consumes, for the
	// collision check at Send (D-E).
	ReservedKeys() []string
}
```

Framing is a separate seam from body grammar, so that a wire's grammar can be
reused under a different transport framing without cloning the codec — Anthropic's
Messages grammar under AWS binary event-stream framing is the motivating case, and
fusing framing into the grammar would force a wire clone. A `Framer` splits a
response body into raw payload frames; SSE is the only day-one implementation, and
it is also exported as a public leaf (`agentkit/…` SSE frame reader) because the
sibling `mcp` reuses it for `text/event-stream` RPC responses (D0, D17).

```go
// Framer splits a response body into raw payload frames. The wire's
// DecodeStream consumes the sequence. SSE is the only day-one Framer; AWS
// event-stream framing would be another, over an unchanged grammar.
type Framer func(io.Reader) iter.Seq2[[]byte, error]

// SSEFrames is the day-one Framer: it reads Server-Sent-Events data payloads,
// skipping comment keep-alives and honoring a terminal sentinel where the wire
// uses one. Exported as a public leaf for reuse by sibling mcp.
func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]
```

Each wire's conformance is pinned by one property that single-turn tests miss: a
build-side/parse-side asymmetry that only bites on turn 2. The obligation is a
round-trip — parse a captured fixture into a `Message`, assemble it back into a
request body, and assert byte-equality against the fixture's own input bytes — run
per wire against golden `testdata/*.sse`. Vendor byte facts (field names, header
names, the terminal sentinel) live in those fixtures and in conformance tests, not
in requirement text; the requirements below fix the seam's shape.

## REQUIREMENTS

- R-2WCZ-48BW: A `WireFormat` MUST be selectable only through a constructor and MUST NOT be an assignable field on a `Conversation` or any consumer-visible value.
- R-YB1L-L7DS: A `WireFormat` MUST own the request body grammar, the streaming event vocabulary, the usage location and subset topology, the tool declaration and tool-result shapes, and reasoning replay in full — both its mechanics and its body encoding; it MUST NOT own base URL, auth, headers, or the error envelope.
- R-2YSR-VRTA: `DecodeStream` MUST terminate framing decode within the wire and yield only message-granular events; no framing artifact may reach the orchestrator.
- R-300O-9JJZ: `DecodeStream` MUST merge `Usage` field-wise with each field treated as absolute and last-non-absent winning, and MUST NOT replace usage as a whole object.
- R-318K-NBAO: An in-band vendor error arriving after a 2xx status MUST be surfaced through `DecodeStream`'s error channel (the classifier MUST be reachable from inside the decode).
- R-33OD-EUS2: The SSE frame reader MUST be exported as a public leaf usable independently of any `WireFormat` (for sibling `mcp`).
- R-34W9-SMIR: `RenderTools` MUST reject a tool schema outside the canonical subset (D9) before a request is sent.
- R-3646-6E9G: Every shipped wire MUST satisfy a round-trip property test: parsing a fixture into a `Message` and re-assembling the request body MUST reproduce the fixture's input bytes exactly.
- R-YC9H-YZ4H: `agentkit` MUST export the `WireFormat` interface whose method set is exactly `EncodeRequest(state RequestState) ([]byte, error)`, `DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error]`, `RenderTools(tools []Tool) (json.RawMessage, error)`, and `ReservedKeys() []string`.
- R-ZGPR-FPAQ: `agentkit` MUST export `type Framer func(io.Reader) iter.Seq2[[]byte, error]`.
- R-ZHXN-TH1F: `agentkit` MUST export `func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]`, assignable to `Framer`.
- R-0UPN-4AP7: Framing MUST be separable from body grammar: a `WireFormat` codec MUST run under any `Framer` without modification.
