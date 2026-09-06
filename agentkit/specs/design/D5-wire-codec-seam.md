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
scope). Eight wires ship:

| Constructor | Struct | `WireName` (D21) | Credential placement | Used by hosts |
|---|---|---|---|---|
| `AnthropicMessagesWire()` | `anthropicMessagesWire` | `messages` | `x-api-key` header; also sets `anthropic-version` | anthropic |
| `GeminiGenerateContentWire()` | `geminiGenerateContentWire` | `generate-content` | `key` query parameter | gemini |
| `ChatWire()` | `chatWire` | `chat` | `Authorization: Bearer` | openrouter |
| `ResponsesWire()` | `responsesWire` | `responses` | `Authorization: Bearer` | openrouter |
| `OpenAIChatWire()` | `openAIChatWire` | `chat` | bearer | openai |
| `OpenAIResponsesWire()` | `openAIResponsesWire` | `responses` | bearer, plus `ChatGPT-Account-Id` under OAuth | openai |
| `XAIChatWire()` | `xaiChatWire` | `chat` | `Authorization: Bearer` | xai |
| `XAIResponsesWire()` | `xaiResponsesWire` | `responses` | `Authorization: Bearer` | xai |

**Every wire is its own struct, and the struct is named for its grammar.**
Each constructor returns a distinct type `<x>Wire`, declared as
`struct{ wireCodec }` with `wireCodec` as its sole, embedded field: the
generic wires are `<format>Wire`, a vendor's variant is `<provider><format>Wire`.
Names carry the design: `anthropicMessagesWire` says there is a Messages
grammar and that Anthropic may have others (it has, and will again), where a
name like `anthropicWire` would claim the whole vendor and collide with the
next format. The rule is enforced on unexported types on purpose, overriding
the usual public-only scope, for the same reason as the file rule: it is how
a reader identifies a wire, and how the catalog test tells an xAI wire from
the generic wire it otherwise resembles. There are no derived wires: a vendor
variant that shares a family's grammar shares the encode and decode
*functions*, not a struct, so bodies stay byte-identical without a type
hierarchy.

The OpenAI pair is the generic pair with one more header on the responses
side; the xAI pair is the generic pair with one more error classification
(below). Body grammar and decode are identical across each family of three,
and a test pins that.

**A rejected credential is a wire classification.** The wire owns the
classification of its vendor's error responses (D4), and one classification
matters beyond the `Category`: whether the vendor refused the credential
itself, as opposed to refusing the request for any other reason. That is what
the OAuth re-issue path (D22) needs to know, and only the wire can say it,
because each vendor says it differently. Both facts below were captured live
on 2026-09-05 and contradict the vendors' published error tables, which is
why neither is assumed:

- xAI answers a present-but-invalid OAuth token, expired or with a bad
  signature, with **`403`** and the body
  `{"code":"unauthenticated:bad-credentials","error":"The OAuth2 access token could not be validated."}`
  on both `/v1/responses` and `/v1/chat/completions`. Its `401` is reserved
  for a request with no credential at all
  (`{"code":"unauthenticated:no-credentials",...}`), which no rotation can
  fix; a malformed bearer or a bad API key is `400 invalid-argument`; and a
  valid credential asking for an unknown model is `404 not-found`. The
  published table says an invalid token is `401` and `403` is a permission
  problem; for the OAuth path that is not what the host does.
- OpenAI's Codex responses endpoint answers a present-but-invalid token with
  **`401`** (`{"error":{"code":"unauthorized_unknown",...},"status":401}`
  for a bad signature); `api.openai.com` answers a bad API key with `401`
  `invalid_api_key`. Its `403` is documented as a region block. The
  expired-token body on Codex was not captured (no expired token was
  available); the vendor's own client recovers on any `401`, and so does
  this wire.

So `XAIResponsesWire()` and `XAIChatWire()` classify exactly a `403` whose
JSON body carries `code` equal to `unauthenticated:bad-credentials` as a
rejected credential, and `OpenAIResponsesWire()` classifies exactly a `401`.
The generic wires and the other built-ins classify nothing as a rejected
credential: no offering that speaks them lists an OAuth endpoint, so the
classification would have no observable effect and no live proof. The
classification is not a field on `*Error`; its one observable effect is the
D22 re-issue, and that is how the requirements below are tested: a fake
server answering the vendor's exact body through `Send` under an OAuth
authenticator, asserting whether a rotation happened.

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
regardless of how many trailing chunks repeat a `finish_reason`. OpenRouter rides the generic
wires unchanged, and xAI's pair differs from them only in error classification,
which is why wire and endpoint are separate axes (D6).

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
// consumer can pass a value from one of the eight constructors to New and can
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

- R-IKHO-5TZ4: A wire codec MUST be selectable only by passing a `WireFormat` value obtained from one of the eight root constructors to `New`, and MUST NOT be an assignable field on a `Conversation` or any consumer-visible value.
- R-OWR1-R4WG: A wire codec MUST own the request body grammar, the streaming event vocabulary, the usage location and subset topology, the tool declaration and tool-result shapes, reasoning replay in full — both its mechanics and its body encoding — the option vocabulary it accepts and each option's body encoding (D8), the classification of its vendor's error responses (D4), the vendor's required protocol headers, and the placement of a credential on the request; it MUST NOT own the base URL or hold a credential.
- R-OXYY-4WN5: The `WireFormat` interface MUST declare the exported method `OptionSpecs() []OptionSpec` and MUST NOT declare `ReservedKeys`.
- R-K4E5-D036: Every request built with `AnthropicMessagesWire()` MUST carry the header `anthropic-version: 2023-06-01`.
- R-IQL6-2OOL: Every request body produced by `AnthropicMessagesWire()`, `ChatWire()`, `OpenAIChatWire()`, `XAIChatWire()`, `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()` MUST carry the top-level field `"stream":true`, pinned by the golden request fixtures.
- R-IRT2-GGFA: Every request body produced by `ChatWire()`, `OpenAIChatWire()`, and `XAIChatWire()` MUST carry the top-level field `"stream_options":{"include_usage":true}`, pinned by the golden request fixture.
- R-IT0Y-U85Z: Every request body produced by `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()` MUST carry the top-level field `"store":false`, pinned by the golden request fixture.
- R-JULI-QU56: `DecodeStream` on `ChatWire()` and `OpenAIChatWire()` MUST merge a `usage` object carried on a chunk that also carries a choice with a non-null `finish_reason`, MUST merge one carried on a later chunk with empty `choices`, and MUST yield exactly one `MessageDone` per stream however many chunks carry a `finish_reason`; pinned by golden fixtures in both placements.
- R-ILPK-JLPT: Every request built by any of the eight shipped wires MUST carry the header `Content-Type: application/json`.
- R-E74N-I1F2: `DecodeStream` on `AnthropicMessagesWire()` MUST read `output_tokens` from the `usage` object at the top level of the `message_delta` event, beside `delta`, so that a stream's merged `Usage` carries both the `message_start` input tokens and the `message_delta` output tokens; pinned by golden fixtures carrying `usage` in that placement.
- R-E8CJ-VT5R: `DecodeStream` on `GeminiGenerateContentWire()` MUST carry a `functionCall` part's `thoughtSignature` in the resulting `ToolUse` block's `Provider` payload, and `EncodeRequest` MUST replay it as the `thoughtSignature` of the `functionCall` part rendered for that `ToolUse`; pinned by a golden fixture carrying a signature.
- R-SBI8-RVG3: `DecodeStream` on `ChatWire()` and `OpenAIChatWire()` MUST size `OutputTokens` from a chunk's `usage` object by its `total_tokens`: when `prompt_tokens + completion_tokens == total_tokens`, `OutputTokens` MUST be `completion_tokens − reasoning_tokens`; otherwise, when `prompt_tokens + completion_tokens + reasoning_tokens == total_tokens`, `OutputTokens` MUST be `completion_tokens`; in every other case, including an absent `total_tokens`, `OutputTokens` MUST be `completion_tokens − reasoning_tokens`; `ReasoningTokens` MUST be `reasoning_tokens` in every case; pinned by golden fixtures in both the nested and the disjoint topology.
- R-IPD9-OWXW: For every request state that all wires of a family accept, `ChatWire()`, `OpenAIChatWire()`, and `XAIChatWire()` MUST produce byte-identical request bodies, as MUST `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()`; each family MUST return identical `OptionSpecs()`; and for the same frames each family MUST yield identical events.
- R-2YSR-VRTA: `DecodeStream` MUST terminate framing decode within the wire and yield only message-granular events; no framing artifact may reach the orchestrator.
- R-300O-9JJZ: `DecodeStream` MUST merge `Usage` field-wise with each field treated as absolute and last-non-absent winning, and MUST NOT replace usage as a whole object.
- R-318K-NBAO: An in-band vendor error arriving after a 2xx status MUST be surfaced through `DecodeStream`'s error channel (the classifier MUST be reachable from inside the decode).
- R-O9F8-EXZT: The SSE frame reader MUST be exported as a public leaf usable independently of any wire codec (for sibling `mcp`).
- R-34W9-SMIR: `RenderTools` MUST reject a tool schema outside the canonical subset (D9) before a request is sent.
- R-3646-6E9G: Every shipped wire MUST satisfy a round-trip property test: parsing a fixture into a `Message` and re-assembling the request body MUST reproduce the fixture's input bytes exactly.
- R-ZGPR-FPAQ: `agentkit` MUST export `type Framer func(io.Reader) iter.Seq2[[]byte, error]`.
- R-ZHXN-TH1F: `agentkit` MUST export `func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]`, assignable to `Framer`.
- R-IU8V-7ZWO: `XAIResponsesWire()` and `XAIChatWire()` MUST classify a response as a rejected credential exactly when its status is `403` and its body is a JSON object whose `code` field is the string `unauthenticated:bad-credentials`; a `403` with any other body, and a response of any other status including `401`, MUST NOT be classified as a rejected credential, the classification being observable as whether the D22 re-issue path rotates.
- R-IVGR-LRND: `OpenAIResponsesWire()` MUST classify a response as a rejected credential exactly when its status is `401`, whatever its body; a response of any other status, including `403`, MUST NOT be classified as a rejected credential, the classification being observable as whether the D22 re-issue path rotates.
- R-IMXG-XDGI: Each exported wire constructor `<X>Wire` MUST be declared in the file `wire_<x>.go` (`<x>` being `<X>` in snake_case) — `AnthropicMessagesWire` in `wire_anthropic_messages.go`, `GeminiGenerateContentWire` in `wire_gemini_generate_content.go`, `ChatWire` in `wire_chat.go`, `ResponsesWire` in `wire_responses.go`, `OpenAIChatWire` in `wire_openai_chat.go`, `OpenAIResponsesWire` in `wire_openai_responses.go`, `XAIChatWire` in `wire_xai_chat.go`, `XAIResponsesWire` in `wire_xai_responses.go` — so a constructor and its file share the same `<X>`.
- R-HC0C-ZEW7: Each of the eight root wire constructors MUST return a `WireFormat` whose dynamic type is a pointer to a distinct unexported struct declared as `struct{ wireCodec }` with `wireCodec` as its sole, embedded field, named for the constructor: `AnthropicMessagesWire()` → `anthropicMessagesWire`, `GeminiGenerateContentWire()` → `geminiGenerateContentWire`, `ChatWire()` → `chatWire`, `ResponsesWire()` → `responsesWire`, `OpenAIChatWire()` → `openAIChatWire`, `OpenAIResponsesWire()` → `openAIResponsesWire`, `XAIChatWire()` → `xaiChatWire`, `XAIResponsesWire()` → `xaiResponsesWire`; and `agentkit` MUST declare no other type that embeds `wireCodec`.
