# D1-layout-and-provider-seam

`agentkit` is a Go library that talks to LLM chat/completions APIs and runs an
agentic tool loop. Its import path is `github.com/ikigenba/ikigenba/agentkit`,
with a public leaf package `agentkit/retry` (D14). It is a sub-project in the
ikigenba monorepo.

The core seam is a **three-part decomposition** that replaces the old library's
single "provider" axis. Every conversation is assembled from three orthogonal
pieces, and the whole rest of the design hangs off keeping them separate:

- **`WireFormat`** — the codec: request body shape, streaming framing and
  event vocabulary, where usage sits, tool declaration/result shapes, reasoning
  replay mechanics. It is an exported but sealed interface: a consumer obtains a
  value only from one of four root constructors (`AnthropicMessagesWire()`,
  `OpenAIResponsesWire()`, `OpenAIChatWire()`, `GeminiGenerateContentWire()`) and
  passes it to `New`; it is never an assignable field and cannot be implemented
  outside the root package. Detailed in D5.
- **`Endpoint`** — the opaque transport description: base URL (with any
  model-in-path placement baked in) and the authenticator.
  Detailed in D6.
- **`Model`** — a free-flow string, never gated, passed verbatim. A model released
  today runs with no agentkit release; an unknown model is the vendor's 400, not
  ours.

There are no vendor packages. The catalog (D21) is what knows each host:
an `Offering` carries the wire format, one endpoint spec per credential kind
the host accepts (each with its own URL), and the wire model, and
`Offering.Authenticator` turns a root `Rotator` (D7) into an `Authenticator`.
**The consumer assembles the conversation from root symbols and catalog
data**:

```go
offering, _ := agentkit.Lookup("claude-sonnet-5", "", "")
auth, _     := offering.Authenticator(agentkit.APIKeyRotator(key))
ep, _       := agentkit.NewEndpoint(auth)                      // WithBaseURL(url) to override
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

The one thing a consumer customizes is the URL, through `WithBaseURL`; every
other part of a conversation — the wire codec, the auth mechanism, error
classification, response framing, the HTTP client — is defined inside agentkit.
A new host is a new `OfferingID` and table rows, never consumer code.

The single exported entry point of a conversation is `Send`; everything else is
injected at construction through the `Config` value (D18).

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
	Endpoint string // the offering's OfferingID value, e.g. "openai-responses", "anthropic-messages" (D21)
	AuthMode string // "api_key" or "oauth"
	Model    string // the verbatim model string
}
```

Both provenance fields travel on the `Authenticator` that
`Offering.Authenticator` returns: `Endpoint` is the offering's `OfferingID`,
and `AuthMode` follows the rotator — `"api_key"` for `APIKeyRotator`,
`"oauth"` for `OAuthRotator` (D7). Those are the only two auth modes; there is
no third, and every authenticator comes from an offering.

How this is observed in tests: `Identity` reaches a consumer through the
`conversation` log record (D15) and through `Error.Endpoint`. Tests pin both fields
against bare string literals (`"anthropic-messages"`, `"api_key"`), never against a
production constant — the lint gate rejects an expectation taken from the code
under test, and the literal is the contract anyway. The `"oauth"` case needs no
network: the authenticator
runs while the request is being built, before any HTTP call, so a token source
that returns an error makes `Send` fail with an `*Error` whose `Endpoint`
carries the identity and a `turn_start` record already written to the log
after the `conversation` record `New` wrote.

## REQUIREMENTS

- R-01PL-YNFH: The root package MUST be importable as `github.com/ikigenba/ikigenba/agentkit`.
- R-1S4A-HSUZ: A `Model` MUST be carried and transmitted verbatim as a free-form string with no allow-list, gate, or capability check; an unrecognized model MUST reach the vendor and surface as a vendor error, never a pre-flight rejection.
- R-JJEW-ZZ4H: `Conversation.Send(ctx, ...Block)` MUST carry every block it is given, of any `Block` variant, into the turn's user message.
- R-OJ4C-VPZ1: `agentkit` MUST export the `Conversation` type with the method `func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream`.
- R-YVZG-XLOX: `agentkit` MUST export `type Identity struct { Endpoint string; AuthMode string; Model string }` with exactly those three string fields.
- R-OGOK-46HN: `agentkit` MUST export a `WireFormat` interface together with the argument-less constructors `AnthropicMessagesWire() WireFormat`, `GeminiGenerateContentWire() WireFormat`, `ChatWire() WireFormat`, `ResponsesWire() WireFormat`, `OpenAIChatWire() WireFormat`, `OpenAIResponsesWire() WireFormat`, `XAIChatWire() WireFormat`, and `XAIResponsesWire() WireFormat`, one per built-in wire codec.
- R-LYDD-8J87: A conversation for any cataloged offering MUST be constructible from `New`, `NewEndpoint`, `EndpointOption`, `WithBaseURL`, `Endpoint`, `Authenticator`, `WireFormat` and its constructors, `Rotator`, `APIKeyRotator`, `OAuthRotator`, `Token`, `TokenStore`, `FileTokenStore`, `AuthMode`, `Rotation`, `EndpointSpec`, and `Offering.Authenticator`, and the offering's fields alone.
