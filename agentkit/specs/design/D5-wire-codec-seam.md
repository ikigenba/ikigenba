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
| `OpenAIChatWire()` | `chat` | bearer | openai |
| `OpenAIResponsesWire()` | `responses` | bearer, plus `ChatGPT-Account-Id` under OAuth | openai |

The OpenAI pair is the generic pair with one more header on the responses
side; body grammar and decode are identical, and a test pins that.

**Streaming is requested in the body, and that is a proven vendor fact, not a
default.** Every built-in wire decodes its response as SSE, but only Gemini
asks for a stream through its URL. Anthropic Messages, both chat wires, and
both responses wires answer a request without `"stream":true` with a unary
JSON body, which the SSE reader turns into nothing at all: no events, no
error, zero usage. So each of those wires always sends `"stream":true`. Two
more body facts ride with it, each observed live on every host that speaks
the wire: the chat wires send `"stream_options":{"include_usage":true}`,
because OpenAI and xAI chat streams carry no usage without it (OpenRouter
ignores it); and the responses wires send `"store":false`, because the Codex
backend rejects a request without it and every other responses host accepts
it. Anthropic additionally requires `max_tokens` on every request (D8).

**Chat usage rides on the finish chunk on some hosts.** OpenRouter places
`usage` on a final chunk that still carries a choice with `finish_reason`,
while OpenAI and xAI place it on a trailing chunk with empty `choices`. The
chat decoder therefore reads `usage` from every chunk, including the one that
completes the message, and completes the message exactly once per stream
regardless of how many trailing chunks repeat a `finish_reason`. xAI and OpenRouter ride the generic
wires unchanged, which is why wire and endpoint are separate axes (D6).

**Three more facts the live matrix (D23) proved, each one a request the
hand-written fixtures had wrong.** Every request carries
`Content-Type: application/json`: xAI answers 415 without it, and OpenAI's
chat endpoint cannot parse the body and reports a missing `model` parameter;
Anthropic and Gemini merely tolerate its absence. Anthropic's `message_delta`
event carries `usage` at the top level of the event, beside `delta`, not inside
it, so the wire reads `output_tokens` there and the golden fixtures carry that
placement. Gemini attaches a `thoughtSignature` to a `functionCall` part and
rejects a tool-result turn whose replayed `functionCall` part lacks it, so the
wire keeps the signature as the `ToolUse` block's opaque `Provider` payload (D2)
and replays it on the part.

**Chat usage topology is per host, and `total_tokens` says which.** OpenAI
chat nests `reasoning_tokens` inside `completion_tokens` (prompt 25,
completion 55, reasoning 45, total 80), so the output bucket is completion
minus reasoning. xAI chat reports the same field names with reasoning
*outside* completion (prompt 199, completion 1, reasoning 97, total 297): the
subtraction goes negative and the D3 disjointness invariant breaks. Both were
captured live. The chat decoder therefore does not assume a topology: it
checks `total_tokens` against `prompt + completion` (nested) and against
`prompt + completion + reasoning` (disjoint) and sizes `OutputTokens`
accordingly, nested being the fallback. The wire stays host-unaware; the
answer is in the vendor's own arithmetic.

**What the wire owns**, and nothing else touches: the request body shape; the
streaming framing choice and its event vocabulary; where usage sits in the
response, its field names, and its subset topology (which token buckets nest
inside which — cached ⊂ input, and whether reasoning nests inside output or
sits beside it); the tool
declaration shape and the tool-result shape; whether tool-call arguments travel
as a JSON string or a JSON object; reasoning replay in full — both its mechanics (how a
prior reasoning block is echoed back on the next turn) and its body encoding — and
whether a requested reasoning shape is even expressible on this wire; and the
**option vocabulary** — which wire-neutral option names it accepts, how each
value parses, and which body key each renders to (D8). An earlier revision
exposed `ReservedKeys` for a raw pass-through map's collision check; the
pass-through is gone (D12) and so is the method. The wire also owns the
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
// consumer can pass a value from one of the six constructors to New and can
// ask it to describe its options, but its request-side methods take unexported
// types: wires are defined only inside agentkit.
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

	// OptionSpecs describes the string options this wire accepts in
	// Settings.Options (D8): each wire-neutral name, the value kind that fixes
	// how the user's string is parsed, and a one-line description. It is the
	// wire's whole option vocabulary — a key not listed here fails at Send.
	OptionSpecs() []OptionSpec
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
- R-OWR1-R4WG: A wire codec MUST own the request body grammar, the streaming event vocabulary, the usage location and subset topology, the tool declaration and tool-result shapes, reasoning replay in full — both its mechanics and its body encoding — the option vocabulary it accepts and each option's body encoding (D8), the classification of its vendor's error responses (D4), the vendor's required protocol headers, and the placement of a credential on the request; it MUST NOT own the base URL or hold a credential.
- R-OXYY-4WN5: The `WireFormat` interface MUST declare the exported method `OptionSpecs() []OptionSpec` and MUST NOT declare `ReservedKeys`.
- R-K4E5-D036: Every request built with `AnthropicMessagesWire()` MUST carry the header `anthropic-version: 2023-06-01`.
- R-JPPX-7R6E: Every request body produced by `AnthropicMessagesWire()`, `ChatWire()`, `OpenAIChatWire()`, `ResponsesWire()`, and `OpenAIResponsesWire()` MUST carry the top-level field `"stream":true`, pinned by the golden request fixtures.
- R-JS5P-ZANS: Every request body produced by `ChatWire()` and `OpenAIChatWire()` MUST carry the top-level field `"stream_options":{"include_usage":true}`, pinned by the golden request fixture.
- R-JTDM-D2EH: Every request body produced by `ResponsesWire()` and `OpenAIResponsesWire()` MUST carry the top-level field `"store":false`, pinned by the golden request fixture.
- R-JULI-QU56: `DecodeStream` on `ChatWire()` and `OpenAIChatWire()` MUST merge a `usage` object carried on a chunk that also carries a choice with a non-null `finish_reason`, MUST merge one carried on a later chunk with empty `choices`, and MUST yield exactly one `MessageDone` per stream however many chunks carry a `finish_reason`; pinned by golden fixtures in both placements.
- R-E4OU-QHXO: Every request built by any of the six shipped wires MUST carry the header `Content-Type: application/json`.
- R-E74N-I1F2: `DecodeStream` on `AnthropicMessagesWire()` MUST read `output_tokens` from the `usage` object at the top level of the `message_delta` event, beside `delta`, so that a stream's merged `Usage` carries both the `message_start` input tokens and the `message_delta` output tokens; pinned by golden fixtures carrying `usage` in that placement.
- R-E8CJ-VT5R: `DecodeStream` on `GeminiGenerateContentWire()` MUST carry a `functionCall` part's `thoughtSignature` in the resulting `ToolUse` block's `Provider` payload, and `EncodeRequest` MUST replay it as the `thoughtSignature` of the `functionCall` part rendered for that `ToolUse`; pinned by a golden fixture carrying a signature.
- R-SBI8-RVG3: `DecodeStream` on `ChatWire()` and `OpenAIChatWire()` MUST size `OutputTokens` from a chunk's `usage` object by its `total_tokens`: when `prompt_tokens + completion_tokens == total_tokens`, `OutputTokens` MUST be `completion_tokens − reasoning_tokens`; otherwise, when `prompt_tokens + completion_tokens + reasoning_tokens == total_tokens`, `OutputTokens` MUST be `completion_tokens`; in every other case, including an absent `total_tokens`, `OutputTokens` MUST be `completion_tokens − reasoning_tokens`; `ReasoningTokens` MUST be `reasoning_tokens` in every case; pinned by golden fixtures in both the nested and the disjoint topology.
- R-OZ6U-IODU: For every request state that both wires of a pair accept, `ChatWire()` and `OpenAIChatWire()` MUST produce byte-identical request bodies, as MUST `ResponsesWire()` and `OpenAIResponsesWire()`; each pair MUST return identical `OptionSpecs()`; and for the same frames each pair MUST yield identical events.
- R-2YSR-VRTA: `DecodeStream` MUST terminate framing decode within the wire and yield only message-granular events; no framing artifact may reach the orchestrator.
- R-300O-9JJZ: `DecodeStream` MUST merge `Usage` field-wise with each field treated as absolute and last-non-absent winning, and MUST NOT replace usage as a whole object.
- R-318K-NBAO: An in-band vendor error arriving after a 2xx status MUST be surfaced through `DecodeStream`'s error channel (the classifier MUST be reachable from inside the decode).
- R-O9F8-EXZT: The SSE frame reader MUST be exported as a public leaf usable independently of any wire codec (for sibling `mcp`).
- R-34W9-SMIR: `RenderTools` MUST reject a tool schema outside the canonical subset (D9) before a request is sent.
- R-3646-6E9G: Every shipped wire MUST satisfy a round-trip property test: parsing a fixture into a `Message` and re-assembling the request body MUST reproduce the fixture's input bytes exactly.
- R-ZGPR-FPAQ: `agentkit` MUST export `type Framer func(io.Reader) iter.Seq2[[]byte, error]`.
- R-ZHXN-TH1F: `agentkit` MUST export `func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]`, assignable to `Framer`.
- R-K368-Z8CH: Each exported wire constructor `<X>Wire` MUST be declared in the file `wire_<x>.go` (`<x>` being `<X>` in snake_case) — `AnthropicMessagesWire` in `wire_anthropic_messages.go`, `GeminiGenerateContentWire` in `wire_gemini_generate_content.go`, `ChatWire` in `wire_chat.go`, `ResponsesWire` in `wire_responses.go`, `OpenAIChatWire` in `wire_openai_chat.go`, `OpenAIResponsesWire` in `wire_openai_responses.go` — so a constructor and its file share the same `<X>`.
