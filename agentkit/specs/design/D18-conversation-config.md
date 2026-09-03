# D18-conversation-config

A `Conversation` fixes everything but its transcript at construction (D12): the
provider, the model, the generation settings, the pass-through options, the tool
set, and the event log. Before this document there was no public way to *supply*
most of that — the vendor constructors accepted only transport options, and the one post-construction method, `Deferred`, contradicted the
"fixed at construction" rule it lived beside. `Config` closes the gap: one neutral
value that carries every construction-time input, handed to the constructor and
copied once, so nothing about the conversation's configuration can move after
`New` returns.

```go
package agentkit

// Config is the construction-time configuration of a Conversation: everything a
// consumer supplies that is not the provider, the model, or the transport. It is
// a plain value; a zero Config means no tools, vendor-default settings, no
// pass-through options, no structured output, and no log. The constructor copies
// it, so later mutation of the caller's slices and maps has no effect.
type Config struct {
	Tools    []Tool          // eager tools, advertised on every round-trip (D9, D11)
	Deferred []DeferredGroup // on-demand tool groups behind load_tools (D16)
	Settings Settings        // generation settings and reasoning shape (D8)
	Options  ProviderOptions // wire-specific pass-through, collision-checked at Send (D12)
	Output   *OutputContract // structured-output contract, nil for none (D20)
	Log      *Log            // event log, nil for none (D15)
}
```

The root constructor takes `Config` positionally. It is the assembly seam the
vendor packages call (D1), not a consumer route, and an empty `Config{}` in the
call is an honest statement that the caller wants nothing attached:

```go
package agentkit

func New(wire KnownWire, endpoint Endpoint, model string, cfg Config) (*Conversation, error)
```

There is exactly one root constructor. An earlier revision also exported a
constructor that injected a consumer-implemented provider; it had no caller
outside the test suite and is gone (D1). Tests inside the package that need a
fake provider use an unexported constructor.

Each vendor package takes the same value through one option in its own `Option`
type, so the common two-argument call stays two arguments and the D17 rule that
every module defines its own option type holds:

```go
package anthropic // and openai, gemini, xai, openrouter alike

func WithConfig(cfg agentkit.Config) Option

conv, err := anthropic.New(cred, "claude-sonnet-4-5",
	anthropic.WithConfig(agentkit.Config{Tools: tools, Settings: settings}))
```

`Deferred` as a method is gone. Deferred groups are the `Deferred` field, and the
`Conversation` exported method set shrinks to `Send` alone — the one verb (D1).
Everything D16 says about deferred tools (the catalog, `load_tools`, monotonic
loading, cache-stable ordering) is unchanged; only the registration door moved.

## REQUIREMENTS

- R-SKM2-6G5E: `agentkit` MUST export `type Config struct { Tools []Tool; Deferred []DeferredGroup; Settings Settings; Options ProviderOptions; Output *OutputContract; Log *Log }` with exactly those fields.
- R-OKEB-UVO2: `agentkit` MUST export `func New(wire KnownWire, endpoint Endpoint, model string, cfg Config) (*Conversation, error)` as the sole root constructor, taking the wire, endpoint, model, and config as required positional parameters with no functional options, and MUST return `ErrInvalidConfig` for a `KnownWire` value outside the declared constants.
- R-SO9R-BRDH: Each vendor package (`anthropic`, `openai`, `gemini`, `xai`, `openrouter`) MUST export `func WithConfig(cfg agentkit.Config) Option` in its own `Option` type, and `New` MUST accept it alongside the package's transport options.
- R-SPHN-PJ46: The exported method set of `Conversation` MUST be exactly `Send`; in particular no `Deferred` method and no other post-construction attach method may exist.
- R-SQPK-3AUV: A `Conversation` built from a zero `Config` MUST advertise no tools, request vendor defaults for every generation control, send no pass-through options, declare no structured output, and write no log.
- R-SRXG-H2LK: The constructor MUST copy `Config` such that mutating the caller's `Tools`, `Deferred`, `Options`, or `Settings.StopSequences` after construction has no observable effect on any subsequent `Send`.
- R-ST5C-UUC9: `Config.Tools` MUST be the eager tool set: every tool in it MUST be advertised on every round-trip of every turn and MUST be dispatchable by name (D11).
- R-SUD9-8M2Y: `Config.Deferred` MUST be the sole registration of deferred tool groups, and a non-empty `Config.Deferred` MUST cause the orchestrator to synthesize `load_tools` exactly as D16 specifies.
- R-OMU4-MF5G: `Config.Settings` and `Config.Options` MUST be the `Settings` and `Options` the wire codec encodes on every round-trip of every turn, unchanged across the conversation's life.
- R-SY0Y-DXB1: `Config.Log` MUST be the event log written for every turn of the conversation, and a nil `Config.Log` MUST write nothing.
- R-OLM8-8NER: A `Conversation` built by a vendor package's `New` with `WithConfig(cfg)` and one built by the root `New` with the same wire, endpoint, model, and `cfg` MUST be behaviorally identical.
- R-9GCK-P42P: Passing a `Config` whose `Tools`, `Deferred`, or `Options` fail their `Send`-time gates (D11, D12) MUST NOT fail construction; the fault MUST surface from `Send` as `ErrInvalidConfig` with no provider call and `History` unchanged.
