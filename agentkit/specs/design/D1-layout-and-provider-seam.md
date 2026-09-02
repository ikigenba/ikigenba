# D1-layout-and-provider-seam

`agentkit` is a Go library that talks to LLM chat/completions APIs and runs an
agentic tool loop. Module path `github.com/ikigenba/ikigenba/agentkit`, its own
`go.mod`, `go 1.26`, no `go.work`; every command runs from this sub-project
directory. It is a sub-project in the ikigenba monorepo and mirrors idgen's
house layout:

```
agentkit/                               (this sub-project; go.mod lives here)
├── AGENTS.md                           spec-driven build contract, gates
├── README.md
├── Makefile                            build test lint llm-lint fmt clean install
├── go.mod                              go 1.26; no go.work
├── .golangci.yml                       version 2; standard + errorlint, gocritic, …
├── .llm-lint.json                      promotion allowlist
├── lint-rules/                         llm-lint rule files
├── specs/
│   ├── design/D<int>-<slug>.md         these documents
│   └── loops/{gather,build,verify}.md  + executable run
├── agentkit.go, conversation.go, …     the root package (public surface)
├── retry/                              public leaf: stdlib-only retry (D14)
├── anthropic/ openai/ gemini/ …        vendor constructor packages
└── internal/…                          wire codecs, endpoint plumbing
```

The core seam is a **three-part decomposition** that replaces the old library's
single "provider" axis. Every conversation is assembled from three orthogonal
pieces, and the whole rest of the design hangs off keeping them separate:

- **`WireFormat`** — the internal codec: request body shape, streaming framing and
  event vocabulary, where usage sits, tool declaration/result shapes, reasoning
  replay mechanics. It is never an assignable field; a constructor selects one of
  four (Anthropic Messages, OpenAI Responses, OpenAI Chat Completions, Gemini
  generateContent). Detailed in D5.
- **`Endpoint`** — the public, opaque, option-built transport: base URL and path,
  auth applier, extra headers, framing, error classifier, request-mutation hook,
  and the HTTP client. Detailed in D6.
- **`Model`** — a free-flow string, never gated, passed verbatim. A model released
  today runs with no agentkit release; an unknown model is the vendor's 400, not
  ours.

Vendor packages are named constructors that bake a `(WireFormat, Endpoint)` pair
with their own closed, typed credential set — `anthropic.New(...)`,
`openai.New(...)`, and so on (credentials in D7). Unlike the old library, the
generic wire path is **public**: a consumer can construct a `(wire, endpoint,
model)` triple directly, so a brand-new vendor speaking a known wire needs no code
in agentkit. Compile-time credential safety lives only on the vendor path (each
package's unexported `isCredential()`); on the generic path a custom base URL's
vendor is unknowable, so no such safety can exist — and that is correct.

The single exported entry point of a conversation is `Send`; everything else is
injected at construction through the vendor constructor's options or, on the
generic path, the `Config` value (D18). The constructor signatures are declared
in D18. Dependencies point one way: vendor package →
`Conversation` → `WireFormat`/`Endpoint` → the wire codec and transport. Nothing
below `Conversation` reaches back up.

```go
package agentkit

// Send drives one turn: it appends the user blocks, calls the model, runs any
// tool round-trips to completion, and returns a Stream of message-granular
// events (D13). Provider, endpoint, and model are fixed for the conversation's
// life; only the transcript grows. Send is the one verb — multimodal input
// arrives as additional Block variants, never as a second method.
func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream
```

The library states an explicit **escape-hatch ladder** so "we can always graft a
new API" is credible rather than hopeful. Each rung is more work than the last and
none is a dead end: (1) a **vendor constructor** for a supported vendor; (2) the
**generic wire constructor** for a new vendor speaking one of the four known
wires; (3) **dialect hooks** on `Endpoint` — base URL, auth applier, extra
headers, framing, error classifier, request-mutation hook — for a
vendor that bends a known wire; (4) a custom `http.RoundTripper` injected through
the endpoint for transport-level needs; (5) implement the public `Provider` SPI
yourself for a genuinely new wire. The last rung is only credible if the SPI is
public and small, so it is: an adapter written entirely outside agentkit is a
first-class citizen, constructed and passed in like any vendor's.

```go
package agentkit

// Provider is the composed (WireFormat, Endpoint) SPI the orchestrator drives
// for one round-trip: build the request from the conversation state, decode the
// transport's framed response into message-granular events, and classify an
// error response. It is small and public so a new wire can be implemented
// outside agentkit and injected like any vendor's; the four built-in wires
// implement it internally. Ctx, the HTTP client, the error classifier, and the
// clock are injected — Provider owns no globals. Its method set is fixed in D6;
// it is restated here as the last rung of the escape-hatch ladder.
type Provider interface {
	// BuildRequest assembles the wire body (D5) for the turn, wraps it against
	// the endpoint (base URL, path, model placement, headers), then applies the
	// RequestMutator and the AuthApplier in that order (D6).
	BuildRequest(ctx context.Context, state RequestState) (*http.Request, error)
	// Decode frames the response (endpoint Framer) and decodes it (wire
	// DecodeStream) into the turn's events in order, terminating stream decoding
	// in the adapter (no token deltas escape).
	Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]
	// Classify applies the endpoint's ErrorClassifier (D4, D6).
	Classify(status int, header http.Header, body []byte) error
	// Identity reports the endpoint identity, auth mode, and model this provider
	// speaks for, for the log and cost paths (D3, D15).
	Identity() Identity
}
```

`RequestState` (the immutable config plus the transcript snapshot handed to a
round-trip) is defined by the orchestrator in D12; `Event` is the message-granular
decode output defined in D13. `Identity` is the conversation's stable endpoint
provenance, defined here because it is the Provider seam's own return type and is
consumed by the error (D4), cost (D3), and log (D15) paths. It carries endpoint
identity, auth mode, and model as separate fields — there is no single fused
provider id — so a log consumer can filter "every OpenAI turn" and "every
OAuth-paid turn" independently.

```go
package agentkit

// Identity is a conversation's stable provenance: which endpoint it speaks to,
// under which auth mode, for which model. The three are separate fields, never a
// fused string, so consumers filter on each independently (D15). It is immutable
// for the conversation's life.
type Identity struct {
	Endpoint string // endpoint name, e.g. "openai", "openai.oauth", "xai"
	AuthMode string // "api_key", "oauth", "sigv4"
	Model    string // the verbatim model string
}
```

## REQUIREMENTS

- R-1OGL-CHMW: The module MUST declare path `github.com/ikigenba/ikigenba/agentkit` in its own `go.mod` at `go 1.26`, with no `go.work` file, so every gate and command runs from the `agentkit/` directory.
- R-1POH-Q9DL: A `Conversation` MUST be constructed from exactly three orthogonal parts — a `WireFormat`, an `Endpoint`, and a `Model` string — and MUST expose no method to reassign the wire, endpoint, or model after construction.
- R-1S4A-HSUZ: A `Model` MUST be carried and transmitted verbatim as a free-form string with no allow-list, gate, or capability check; an unrecognized model MUST reach the vendor and surface as a vendor error, never a pre-flight rejection.
- R-1TC6-VKLO: `Conversation.Send(ctx, ...Block)` MUST be the sole verb for advancing a conversation, and additional input modalities MUST be expressible as new `Block` variants without adding a second send method.
- R-1UK3-9CCD: A vendor constructor and the generic wire constructor MUST both yield a `Conversation` that is behaviorally identical apart from credential typing, so the generic `(WireFormat, Endpoint, Model)` path is a fully public first-class construction route.
- R-1VRZ-N432: The `Provider` SPI MUST be exported and implementable outside the module, and a conforming external implementation injected at construction MUST drive a `Conversation` through `Send` with no agentkit source change.
- R-1WZW-0VTR: Dependencies MUST point one way — vendor package → `Conversation` → `WireFormat`/`Endpoint` → codec/transport — verified by the absence of any import from a lower layer back to `Conversation` or a vendor package.
- R-YURK-JTY8: `agentkit` MUST export `Conversation` as an opaque struct type with no exported fields, exposing the method `func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream`.
- R-YVZG-XLOX: `agentkit` MUST export `type Identity struct { Endpoint string; AuthMode string; Model string }` with exactly those three string fields.
- R-Y7DW-FW5P: `agentkit` MUST export `type KnownWire int` with the constants `KnownWireAnthropicMessages`, `KnownWireOpenAIResponses`, `KnownWireOpenAIChat`, `KnownWireGemini` declared in that `iota` order starting at 0, enumerating the built-in wires selectable on the generic construction path.
