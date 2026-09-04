# D5-wire-codec-seam

A **wire codec** is the codec for one vendor's HTTP body grammar and streaming
event vocabulary. Consumers see it only as the sealed `WireFormat` interface (D1):
its methods are unexported, so a value can be held and passed but never
implemented outside the root package, and it is never an assignable field on a
`Conversation`. A consumer obtains one from a catalog offering's `WireFormat` field or from an
argument-less root constructor named `<X>Wire()` and hands it to `New`,
which pairs it with an `Endpoint` (D6) for the orchestrator. Each constructor
`<X>Wire` lives in `wire_<x>.go`, so the constructor and its file always read the
same (the file rule names layout on purpose, overriding the usual public-only
scope). Six wires ship:

| Constructor | `WireName` (D21) | Credential placement | Used by hosts |
|---|---|---|---|
| `AnthropicMessagesWire()` | `messages` | `x-api-key` header; also sets `anthropic-version` | anthropic |
| `GeminiGenerateContentWire()` | `generate-content` | `key` query parameter | gemini |
| `ChatWire()` | `chat` | `Authorization: Bearer` | xai, openrouter |
| `ResponsesWire()` | `responses` | `Authorization: Bearer` | xai, openrouter |
| `OpenAIChatWire()` | `chat` | bearer, plus `ChatGPT-Account-Id` under OAuth | openai |
| `OpenAIResponsesWire()` | `responses` | bearer, plus `ChatGPT-Account-Id` under OAuth | openai |

The OpenAI pair is the generic pair with one more header; body grammar and
decode are identical, and a test pins that. xAI and OpenRouter ride the generic
wires unchanged, which is why wire and endpoint are separate axes (D6).

**What the wire owns**, and nothing else touches: the request body shape; the
streaming framing choice and its event vocabulary; where usage sits in the
response, its field names, and its subset topology (which token buckets nest
inside which — cached ⊂ input, reasoning ⊂ output, and so on); the tool
declaration shape and the tool-result shape; whether tool-call arguments travel
as a JSON string or a JSON object; reasoning replay in full — both its mechanics (how a
prior reasoning block is echoed back on the next turn) and its body encoding — and
whether a requested reasoning shape is even expressible on this wire. The wire also owns the
classification of its vendor's error responses into `Category` (D4), because the
error envelope is part of the vendor's grammar. The wire also owns the
**protocol headers**: every header the vendor's HTTP protocol requires
(`anthropic-version`) and **where a credential is placed** on the request —
which header or query parameter carries the secret. The secret itself, and its
lifecycle, belong to the `Authenticator` (D6, D7): the authenticator resolves
the current credential and the wire places it. The base URL belongs to the
`Endpoint` (D6). The dividing question is "does this change the bytes the
vendor parses — body or headers — or only where they are sent and what secret
is used?"

The codec is one interface. Encoding assembles a request body from the turn
state; decoding assembles message-granular protocol events from framed chunks and
terminates SSE (or other framing) decode here — no framing detail escapes into
the orchestrator. Usage merges field-wise, each field absolute, last-non-absent
wins (never whole-object last-wins), which is a decode-side invariant of the wire.

```go
// WireFormat is the codec for one vendor body grammar. It is exported so a
// consumer can pass a value from one of the four constructors to New, but its
// methods are unexported: wires are defined only inside agentkit.
type WireFormat interface {
	// EncodeRequest renders the assembled turn state into a request body for
	// this wire. Base URL is the Endpoint's concern (D6); EncodeRequest
	// produces body bytes only. Protocol headers and credential placement
	// are applied by the wire through an unexported hook, not here.
	EncodeRequest(state requestState) ([]byte, error)

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

Framing is a separate seam from body grammar inside the library, so that a
wire's grammar could be reused under a different transport framing without
cloning the codec. A `Framer` splits a response body into raw payload frames;
SSE is the only implementation, every built-in wire runs under it, and a consumer
cannot substitute another. The `Framer` type and the SSE reader are exported as a
public leaf (`agentkit/…` SSE frame reader) because the
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

- R-K0QG-7OV3: A wire codec MUST be selectable only by passing a `WireFormat` value obtained from one of the six root constructors to `New`, and MUST NOT be an assignable field on a `Conversation` or any consumer-visible value.
- R-K1YC-LGLS: A wire codec MUST own the request body grammar, the streaming event vocabulary, the usage location and subset topology, the tool declaration and tool-result shapes, reasoning replay in full — both its mechanics and its body encoding — the classification of its vendor's error responses (D4), the vendor's required protocol headers, and the placement of a credential on the request; it MUST NOT own the base URL or hold a credential.
- R-K4E5-D036: Every request built with `AnthropicMessagesWire()` MUST carry the header `anthropic-version: 2023-06-01`.
- R-K5M1-QRTV: For the same request state, `ChatWire()` and `OpenAIChatWire()` MUST produce byte-identical request bodies, as MUST `ResponsesWire()` and `OpenAIResponsesWire()`; and for the same frames each pair MUST yield identical events.
- R-2YSR-VRTA: `DecodeStream` MUST terminate framing decode within the wire and yield only message-granular events; no framing artifact may reach the orchestrator.
- R-300O-9JJZ: `DecodeStream` MUST merge `Usage` field-wise with each field treated as absolute and last-non-absent winning, and MUST NOT replace usage as a whole object.
- R-318K-NBAO: An in-band vendor error arriving after a 2xx status MUST be surfaced through `DecodeStream`'s error channel (the classifier MUST be reachable from inside the decode).
- R-O9F8-EXZT: The SSE frame reader MUST be exported as a public leaf usable independently of any wire codec (for sibling `mcp`).
- R-34W9-SMIR: `RenderTools` MUST reject a tool schema outside the canonical subset (D9) before a request is sent.
- R-3646-6E9G: Every shipped wire MUST satisfy a round-trip property test: parsing a fixture into a `Message` and re-assembling the request body MUST reproduce the fixture's input bytes exactly.
- R-ZGPR-FPAQ: `agentkit` MUST export `type Framer func(io.Reader) iter.Seq2[[]byte, error]`.
- R-ZHXN-TH1F: `agentkit` MUST export `func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]`, assignable to `Framer`.
- R-K368-Z8CH: Each exported wire constructor `<X>Wire` MUST be declared in the file `wire_<x>.go` (`<x>` being `<X>` in snake_case) — `AnthropicMessagesWire` in `wire_anthropic_messages.go`, `GeminiGenerateContentWire` in `wire_gemini_generate_content.go`, `ChatWire` in `wire_chat.go`, `ResponsesWire` in `wire_responses.go`, `OpenAIChatWire` in `wire_openai_chat.go`, `OpenAIResponsesWire` in `wire_openai_responses.go` — so a constructor and its file share the same `<X>`.
