# D8-generation-settings-and-reasoning

A turn carries generation controls alongside its transcript: sampling knobs, an
output cap, a tool-choice directive, and a reasoning request. These live in a
`Settings` value that the orchestrator folds into `RequestState` (D12); the wire's
`EncodeRequest` (D5) renders whatever subset the wire can express. `Settings` is a
plain value type with no network reach and no vendor vocabulary in it — it is the
neutral shape, and each wire owns the translation to its own body grammar.

```go
package agentkit

// Settings are the generation controls for a turn. Every field is optional: a
// zero Settings requests the vendor's own defaults for everything. Pointer
// fields distinguish "unset" (leave to the vendor) from a deliberate zero (e.g.
// Temperature 0). Settings carries no vendor vocabulary — each wire renders the
// subset it can express (D5) and fails loud on what it cannot (see ReasoningConfig).
type Settings struct {
	// Temperature and TopP are the sampling knobs, passed through when set.
	Temperature *float64
	TopP        *float64
	// MaxOutputTokens caps generated tokens (excluding nothing — the vendor's
	// own accounting decides whether reasoning counts against it).
	MaxOutputTokens *int
	// StopSequences are verbatim stop strings, passed through when non-empty.
	StopSequences []string
	// ToolChoice directs tool selection for the turn (see ToolChoice).
	ToolChoice ToolChoice
	// Reasoning requests model reasoning in a wire-neutral shape (see
	// ReasoningConfig).
	Reasoning ReasoningConfig
}
```

**Reasoning is requested in a neutral internal model and validated for
representability at `Send`.** Vendors express reasoning four incompatible ways: an
*effort* level (low/medium/high), a *token budget* (a `thinkingBudget`-style
integer), a bare *on* with no parameter, and an explicit *off*. Not every wire can
express every shape — one wire takes effort only, another takes budget xor level
but never both, another has no bare-on form at all. agentkit models the request
neutrally and lets each wire declare which shapes it can render; a request a wire
cannot express is a **fail-loud** error at `Send`, never a silent downgrade.

```go
// ReasoningMode is the neutral reasoning request. A wire renders the shapes it
// supports and rejects the rest at Send (ErrInvalidConfig) — agentkit never
// silently rewrites "budget 8000" into "effort high" or drops reasoning to make
// a request fit.
type ReasoningMode int

const (
	ReasoningDefault ReasoningMode = iota // leave reasoning to the vendor default
	ReasoningOff                          // explicitly disable reasoning
	ReasoningOn                           // bare on, no parameter
	ReasoningEffort                       // low/medium/high effort level
	ReasoningBudget                       // an explicit token budget
)

// ReasoningConfig is a neutral reasoning request. Mode selects the shape; Effort
// is read only when Mode is ReasoningEffort and Budget only when Mode is
// ReasoningBudget. A Mode a wire cannot express fails at Send. It is named
// ReasoningConfig, not Reasoning, because the D2 block variant owns the exported
// name Reasoning in package agentkit; the Settings field keeps the name Reasoning.
type ReasoningConfig struct {
	Mode   ReasoningMode
	Effort Effort // low | medium | high
	Budget int    // token budget, when Mode is ReasoningBudget
}

// Effort is the neutral reasoning effort level, read only when a ReasoningConfig's Mode
// is ReasoningEffort. The zero value EffortNone means "not an effort request".
type Effort int

const (
	EffortNone   Effort = iota // unset — not an effort request
	EffortLow
	EffortMedium
	EffortHigh
)

// ToolChoice steers whether, and which, tool the model may call this turn. Like
// ReasoningConfig it is neutral and fail-loud: a wire that cannot express the chosen
// mode rejects it at Send (ErrInvalidConfig), never silently downgrades. The zero
// value ToolChoiceAuto leaves the decision to the model.
type ToolChoice struct {
	Mode ToolChoiceMode
	Name string // the required tool, when Mode is ToolChoiceTool
}

// ToolChoiceMode is the neutral tool-selection directive.
type ToolChoiceMode int

const (
	ToolChoiceAuto     ToolChoiceMode = iota // model decides (default)
	ToolChoiceNone                           // no tool call this turn
	ToolChoiceRequired                       // must call some tool
	ToolChoiceTool                           // must call the tool named in ToolChoice.Name
)
```

The fail-loud rule is deliberate and consistent with the rest of the library: an
out-of-subset tool schema (D9), a reserved `ProviderOptions` key collision (D6,
D12), and a `ToolChoice` a wire cannot express all fail the same way — an
`ErrInvalidConfig` returned from `Send` before any provider call, leaving `History`
unchanged. The library never guesses a "closest" substitute, because a silent
substitution would bill the consumer for a turn they did not ask for and hide the
mismatch until a much later debugging session. Representability is checked against
the wire's declared capability, not against the model: agentkit does **not** gate
on model identity. A model that rejects a shape the wire *can* express returns the
vendor's own 400, surfaced through the classifier (D4). Reasoning that the vendor
returns is carried back as `Reasoning` blocks (D2) and replayed under the
endpoint-owned replay encoding (D6, D-K).

**Model gating is cut entirely.** There is no capability table, no per-model
allow-list, and no pre-flight "does this model support reasoning?" check. The wire
knows only its own body grammar; the model string is opaque and passed verbatim
(D1). Whether a given model honors a shape is the vendor's judgment, delivered as a
200 or a 400 — never agentkit's. This keeps a day-one model working with no
release: only a genuinely new *wire shape* (not a new model) requires library work.

## REQUIREMENTS

- R-3PMK-AQ4K: `Settings` MUST be a plain value type whose fields are all optional, such that a zero `Settings` requests vendor defaults for every control and carries no vendor-specific vocabulary.
- R-3QUG-OHV9: A reasoning request MUST be expressed in a wire-neutral model (default, off, bare-on, effort, budget) that the consumer sets without naming any vendor.
- R-3S2D-29LY: A reasoning shape a target wire cannot express MUST fail at `Send` with `ErrInvalidConfig` before any provider call, and MUST NOT be silently downgraded, substituted, or dropped.
- R-3UI5-TT3C: A `ToolChoice` directive a target wire cannot express MUST fail at `Send` with `ErrInvalidConfig`, consistent with reasoning representability and out-of-subset schemas.
- R-3VQ2-7KU1: Reasoning representability MUST be validated against the wire's declared capability, not against the model string; agentkit MUST NOT maintain a per-model capability gate or allow-list.
- R-3WXY-LCKQ: An unrecognized or unsupported model MUST reach the vendor and surface as a vendor error via the classifier (D4), never as a pre-flight rejection.
