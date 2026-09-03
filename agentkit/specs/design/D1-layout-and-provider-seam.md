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
- **`Endpoint`** — the opaque transport description: base URL (with any
  model-in-path placement baked in) and the auth applier, and nothing else.
  Detailed in D6.
- **`Model`** — a free-flow string, never gated, passed verbatim. A model released
  today runs with no agentkit release; an unknown model is the vendor's 400, not
  ours.

Vendor packages are named constructors that select a built-in wire and build an
`Endpoint` with their own closed, typed credential set — `anthropic.New(...)`,
`openai.New(...)`, and so on (credentials in D7). **A vendor package's `New` is
the only construction route a consumer has.** The one thing a consumer can
customize is the base URL, through each vendor package's `WithBaseURL`; every
other part of a conversation — the wire codec, the auth mechanism, error
classification, response framing, the HTTP client — is defined inside agentkit
and only assembled by the vendor package. A new vendor, or a gateway that pairs
a known wire with an unusual auth style, is a new vendor package inside this
library, never consumer code.

The root package still exports the small seam the vendor packages assemble
with — `New`, `NewEndpoint`, `Endpoint`, `AuthApplier`, `KnownWire` — because
they are separate Go packages and Go has no narrower visibility than "exported".
That seam is an internal assembly contract, not a consumer route: nothing in
this design promises it to consumers, no consumer-implementable interface sits
behind it, and it may later move under `internal/` without changing any
consumer-visible behavior.

The single exported entry point of a conversation is `Send`; everything else is
injected at construction through the vendor constructor's options, which carry
the `Config` value (D18). The constructor signatures are declared in D18.
Dependencies point one way: vendor package → `Conversation` → wire
codec/`Endpoint` → transport. Nothing below `Conversation` reaches back up.

```go
package agentkit

// Send drives one turn: it appends the user blocks, calls the model, runs any
// tool round-trips to completion, and returns a Stream of message-granular
// events (D13). Provider, endpoint, and model are fixed for the conversation's
// life; only the transcript grows. Send is the one verb — multimodal input
// arrives as additional Block variants, never as a second method.
func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream
```

There is no escape-hatch ladder. Earlier revisions of this design promised five
rungs of increasing consumer customization, ending in a consumer-implemented
provider SPI; none of the rungs beyond "vendor constructor plus base URL" ever
had a caller, and each one exported machinery that had to be kept honest. The
seam the orchestrator drives for one round-trip (build the request, decode the
framed response into events, classify an error) exists, but it is unexported and
implemented only by the built-in wires.

`Event` is the message-granular decode output defined in D13. `Identity` is the
conversation's stable endpoint provenance, defined here because it is reported
for every conversation and consumed by the error (D4), cost (D3), and log (D15)
paths. It carries endpoint
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
	Endpoint string // the vendor package's ProviderID value, e.g. "openai", "anthropic", "xai" (D21)
	AuthMode string // "api_key" or "oauth"
	Model    string // the verbatim model string
}
```

Both provenance fields are fixed by the vendor constructor from what it was
handed: `Endpoint` is the package's `ProviderID`, and `AuthMode` follows the
credential — `"api_key"` for an `APIKey` credential, `"oauth"` for an `OAuth`
one (D7). Those are the only two auth modes; there is no third.

## REQUIREMENTS

- R-1OGL-CHMW: The module MUST declare path `github.com/ikigenba/ikigenba/agentkit` in its own `go.mod` at `go 1.26`, with no `go.work` file, so every gate and command runs from the `agentkit/` directory.
- R-O0VX-QJSY: A `Conversation` MUST be constructed from exactly three parts — a built-in wire codec named by `KnownWire`, an `Endpoint`, and a `Model` string — and MUST expose no method to reassign the wire, endpoint, or model after construction.
- R-1S4A-HSUZ: A `Model` MUST be carried and transmitted verbatim as a free-form string with no allow-list, gate, or capability check; an unrecognized model MUST reach the vendor and surface as a vendor error, never a pre-flight rejection.
- R-1TC6-VKLO: `Conversation.Send(ctx, ...Block)` MUST be the sole verb for advancing a conversation, and additional input modalities MUST be expressible as new `Block` variants without adding a second send method.
- R-O23U-4BJN: `agentkit` MUST NOT export any of `NewConversation`, `NewForWire`, `Provider`, `WireFormat`, `RequestState`, `EndpointOption`, `RequestMutator`, `ErrorClassifier`, `WithHeader`, `WithFramer`, `WithClassifier`, `WithMutator`, or `WithHTTPClient`.
- R-O3BQ-I3AC: Dependencies MUST point one way — vendor package → `Conversation` → wire codec/`Endpoint` → transport — verified by the absence of any import from a lower layer back to `Conversation` or a vendor package.
- R-YURK-JTY8: `agentkit` MUST export `Conversation` as an opaque struct type with no exported fields, exposing the method `func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream`.
- R-YVZG-XLOX: `agentkit` MUST export `type Identity struct { Endpoint string; AuthMode string; Model string }` with exactly those three string fields.
- R-UFIH-AUGX: A `Conversation` built by a vendor package's `New` MUST report `Identity.AuthMode` `"api_key"` when constructed with that package's `APIKey` credential and `"oauth"` when constructed with its `OAuth` credential, and MUST produce no other `AuthMode` value.
- R-O4JM-VV11: `agentkit` MUST export `type KnownWire int` with the constants `KnownWireAnthropicMessages`, `KnownWireOpenAIResponses`, `KnownWireOpenAIChat`, `KnownWireGemini` declared in that `iota` order starting at 0, enumerating the built-in wire codecs a vendor package selects at construction.
- R-O5RJ-9MRQ: The root package's exported construction seam MUST be exactly `New`, `NewEndpoint`, `Endpoint`, `AuthApplier`, and `KnownWire`, and every vendor package's `New` MUST build its `Conversation` by calling `NewEndpoint` and then `New`.
