# D8-generation-settings-and-reasoning

A turn carries generation controls alongside its transcript: the user's
sampling and output options, a tool-choice directive, and a reasoning request.
These live in a `Settings` value that the orchestrator folds into
`requestState` (D12); the wire's `EncodeRequest` (D5) renders them in its own
body grammar. `Settings` is a plain value type with no network reach and no
vendor vocabulary in it — it is the neutral shape, and each wire owns the
translation to its own body keys.

**Options are user strings; the wire parses, validates, and describes them.**
The governing principle: nothing but a string is passed in to pick a setting.
An application never hardcodes a temperature or an output cap; the only way a
setting is ever selected is a user's choice, carried unchanged as a
`key=value` pair. So `Settings.Options` is a `map[string]string`, keyed by a
**wire-neutral option name** — `temperature`, `top_p`, `max_output_tokens`,
`stop` — and each wire owns the translation of that name into its own body key
(`max_tokens` on Anthropic Messages, `max_completion_tokens` on OpenAI Chat,
`max_output_tokens` on Responses, `generationConfig.maxOutputTokens` on
Gemini). A key the wire does not know, or a value it cannot parse under the
option's declared kind, fails `Send` with `ErrInvalidConfig` and no provider
call. There is no raw pass-through and no silent forwarding: what reaches the
vendor is exactly what the wire understood.

Because the wire has to understand what it can and cannot do, it can also
**describe** it. `WireFormat.OptionSpecs()` (D5) returns the wire's option
vocabulary — each name, the value shape it accepts, and a one-line description
— so an application can enumerate a wire's options for a help screen and
validate a user's key before it ever builds a conversation.

```go
package agentkit

// Options are the user's generation options, verbatim: option name to the
// string the user supplied. Sampling and output options use wire-neutral
// names; the reasoning option uses the model's own term from the catalog
// (D21) — "effort", "thinking_level", "thinking_budget", or "thinking" — and
// every wire accepts all four. The wire parses each value under the kind its
// OptionSpec declares and rejects, at Send, any key it does not know or any
// value it cannot parse. A nil or empty Options sends no option.
type Options map[string]string

// OptionKind is the value shape an option accepts; it fixes how the wire
// parses the user's string.
type OptionKind int

const (
	OptionKindNumber   OptionKind = iota // a finite decimal number: "0.7", "1", "2e-1"
	OptionKindInteger                    // a base-10 integer: "4096", "-1"
	OptionKindText                       // any string, passed through verbatim
	OptionKindTextList                   // a JSON array of strings: `["END","STOP"]`
	OptionKindReasoning                  // a reasoning choice: "high", "off", "on", "8192", "dynamic"
)

// OptionSpec describes one option a wire accepts: the wire-neutral name a
// consumer uses as the Options key, the kind that fixes the value grammar, and
// a one-line description suitable for help text.
type OptionSpec struct {
	Name        string
	Kind        OptionKind
	Description string
}

// Settings are the generation controls for a turn. A zero Settings requests
// the vendor's own defaults for everything and sends no option. Settings
// carries no vendor vocabulary — each wire renders what it can express (D5)
// and fails loud on what it cannot. Reasoning has no field of its own: it is
// the reasoning option inside Options, like every other user choice.
type Settings struct {
	// Options are the user's string options, including the reasoning choice (see Options).
	Options Options
	// ToolChoice directs tool selection for the turn (see ToolChoice).
	ToolChoice ToolChoice
}
```

The consumer's whole job is to hand every `key=value` pair over unchanged:

```go
conv, err := agentkit.New(offering.WireFormat, ep, offering.WireModel,
	agentkit.Config{Settings: agentkit.Settings{Options: pairs}})
```

The sampling and output options every wire understands, and what each renders
them to:

| Option | Kind | Anthropic Messages | Gemini GenerateContent | Chat (both) | Responses (both) |
|---|---|---|---|---|---|
| `temperature` | number | `temperature` | `generationConfig.temperature` | `temperature` | `temperature` |
| `top_p` | number | `top_p` | `generationConfig.topP` | `top_p` | `top_p` |
| `max_output_tokens` | integer | `max_tokens` | `generationConfig.maxOutputTokens` | `max_completion_tokens` | `max_output_tokens` |
| `stop` | text list | `stop_sequences` | `generationConfig.stopSequences` | `stop` | — |

The Responses grammar has no stop-sequence parameter, so the Responses wires
do not list `stop`, and a user who passes it gets `ErrInvalidConfig` rather
than a body the vendor would reject or ignore. That asymmetry is the point of
the design: the vocabulary is the wire's, discovered from the wire, never
assumed by the application. Whether a value is *acceptable to the vendor* —
Anthropic caps `temperature` at 1, OpenAI at 2 — is the vendor's judgment
delivered as a 400 (D4), exactly as model identity is (below); the wire gates
on grammar, not on range.

**Reasoning is one more option, keyed by the model's term, and validated
for representability at `Send`.** Vendors express reasoning four incompatible
ways: an *effort* level (low/medium/high), a *token budget* (a
`thinkingBudget`-style integer), a bare *on* with no parameter, and an explicit
*off*. Not every wire can express every shape. The user's choice arrives as a
string under the model's own word for the knob — `effort=high`,
`thinking_level=low`, `thinking_budget=8192`, `thinking=off` — and the wire
accepts all four terms as `OptionKindReasoning` options, so the consumer
never has to know which term a model uses. The value grammar is
`ParseReasoning`, which maps the string to the neutral `ReasoningConfig`
without consulting any vendor or model. The term narrows the value: `effort`
and `thinking_level` take a level name, `thinking_budget` an integer,
`thinking` `on`; every term also takes `off` and `dynamic`. Two reasoning
terms in one `Options` is a conflict and fails. The wire then renders the
neutral shape in its own grammar, or fails loud at `Send` if it cannot
express it — never a silent downgrade. Whether the value is inside a
particular *offering's* vocabulary is `ReasoningSpec.Accepts` (D21), which an
application may consult for help text and early validation; agentkit itself
does not gate on the model.

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
// ReasoningBudget. Under ReasoningEffort, Effort's zero value EffortNone is the
// literal "none" level some vendors accept, not an absent value. A Mode a wire
// cannot express fails at Send. It is named ReasoningConfig, not Reasoning,
// because the D2 block variant owns the exported name Reasoning in package
// agentkit; the Settings field keeps the name Reasoning.
type ReasoningConfig struct {
	Mode   ReasoningMode
	Effort Effort // none | minimal | low | medium | high | xhigh | max
	Budget int    // token budget, when Mode is ReasoningBudget; 0 is a legal budget
}

// ParseReasoning is the value grammar of an OptionKindReasoning option:
// "dynamic" → ReasoningDefault, "off" → ReasoningOff, "on" → ReasoningOn, an
// Effort name → ReasoningEffort at that level, a non-negative base-10 integer →
// ReasoningBudget. Anything else is an error naming the input. It consults no
// catalog and no wire. Exported so an application can check a value against
// ReasoningSpec.Accepts (D21) before sending; the wire calls it at Send.
func ParseReasoning(s string) (ReasoningConfig, error)

// String is ParseReasoning's inverse: "dynamic", "off", "on", the effort name,
// or the decimal budget, so an application prints a catalog default the same
// way a user would type it.
func (c ReasoningConfig) String() string

// Effort is the neutral reasoning effort level, read only when a ReasoningConfig's Mode
// is ReasoningEffort. The set is exhaustive over every level any cataloged offering
// accepts (D21); a wire renders the level's lowercase name verbatim, and whether a
// given model honors it is the vendor's judgment, not agentkit's.
type Effort int

const (
	EffortNone    Effort = iota // "none" — the vendor's no-reasoning effort level
	EffortMinimal
	EffortLow
	EffortMedium
	EffortHigh
	EffortXHigh
	EffortMax
)

// String is the level's lowercase name — the text a wire renders and
// ParseReasoning accepts.
func (e Effort) String() string

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

`ToolChoice` stays a typed value rather than a string option because it is
not a user setting: it is the application's orchestration directive for a
turn, chosen by program logic, not by a person at a prompt. `ReasoningConfig`
stays exported because the catalog speaks it (`ReasoningSpec.Default`,
`Accepts`), not because a consumer sets it on `Settings`.

**Which reasoning shapes each wire renders.** The Anthropic Messages wire
expresses off (`thinking: {type: disabled}`), effort (`output_config.effort`),
and budget (`thinking: {type: enabled, budget_tokens}`). The Gemini wire
expresses all four through `generationConfig.thinkingConfig` (`thinkingBudget`
0, −1, or N; `thinkingLevel`). The OpenAI Chat and Responses wires express off
and effort through the effort control (`reasoning_effort` and
`reasoning.effort` respectively), with `none` as the off form, and nothing
else — OpenAI has no bare toggle and no token budget, so `ReasoningOn` and
`ReasoningBudget` fail at `Send` on them.

The generic `ChatWire` and `ResponsesWire` (xai, openrouter) render off and
effort exactly as their OpenAI twins do, so the pair stays byte-identical for
every state both accept (D5), and in addition render the two forms OpenRouter
defines on its `reasoning` object and OpenAI does not: `{"enabled": true}` for
`ReasoningOn` and `{"max_tokens": N}` for `ReasoningBudget`. OpenRouter's
reasoning documentation is the source for the field names (`enabled`,
`max_tokens`, `effort`, `exclude`, with `effort` and `max_tokens` documented as
alternatives, and `effort: "none"` documented as disabling reasoning
entirely). Off is therefore sent as the effort form `none` on both generic
wires rather than as `enabled: false`: it is the documented disable, it is what
the OpenAI twin sends, and a wire does not know whether the model behind it is
toggle-kind. The OpenRouter Responses endpoint is documented as
OpenAI-compatible and its main reasoning page presents the `reasoning` object
as one parameter shared by both endpoints, though its Responses reference
shows only `effort` in examples; `ResponsesWire` renders `enabled` and
`max_tokens` on the same `reasoning` object on that basis, and the live
fixture-capture tests are where the vendor's actual acceptance is recorded.
xAI documents only the effort control on both of its endpoints — no toggle,
no budget — so on an xai endpoint a toggle or budget request is well-formed
on the wire and answered by the vendor (D4), never gated by agentkit. The
catalog invariant R-W8QS-PJR3 (D21) is what pins the generic wires' coverage:
every value in an OpenRouter offering's vocabulary must pass its wire's gate.

The reasoning shapes below are stated over the neutral `ReasoningConfig`;
at `Send` that value is whatever `ParseReasoning` returns for the reasoning
option in `Options`, and an `Options` with no reasoning term is
`ReasoningDefault`.

The fail-loud rule is deliberate and consistent with the rest of the library: an
out-of-subset tool schema (D9), an unknown option key or unparsable value, and
a `ToolChoice` a wire cannot express all fail the same way — an
`ErrInvalidConfig` returned from `Send` before any provider call, leaving `History`
unchanged. The library never guesses a "closest" substitute, because a silent
substitution would bill the consumer for a turn they did not ask for and hide the
mismatch until a much later debugging session. Representability is checked against
the wire's declared capability, not against the model: agentkit does **not** gate
on model identity. A model that rejects a shape the wire *can* express returns the
vendor's own 400, surfaced through the classifier (D4). Reasoning that the vendor
returns is carried back as `Reasoning` blocks (D2) and replayed by the wire in its
own grammar (D5).

**Model gating is cut entirely.** There is no capability table, no per-model
allow-list, and no pre-flight "does this model support reasoning?" check. The wire
knows only its own body grammar; the model string is opaque and passed verbatim
(D1). Whether a given model honors a shape is the vendor's judgment, delivered as a
200 or a 400 — never agentkit's. This keeps a day-one model working with no
release: only a genuinely new *wire shape* (not a new model) requires library work.

## REQUIREMENTS

- R-O755-PYBV: `Settings` MUST be a plain value type such that a zero `Settings` requests vendor defaults for every control, sends no option, and carries no vendor-specific vocabulary.
- R-3QUG-OHV9: A reasoning request MUST be expressed in a wire-neutral model (default, off, bare-on, effort, budget) that the consumer sets without naming any vendor.
- R-3S2D-29LY: A reasoning shape a target wire cannot express MUST fail at `Send` with `ErrInvalidConfig` before any provider call, and MUST NOT be silently downgraded, substituted, or dropped.
- R-3UI5-TT3C: A `ToolChoice` directive a target wire cannot express MUST fail at `Send` with `ErrInvalidConfig`, consistent with reasoning representability and out-of-subset schemas.
- R-3VQ2-7KU1: Reasoning representability MUST be validated against the wire's declared capability, not against the model string; agentkit MUST NOT maintain a per-model capability gate or allow-list.
- R-3WXY-LCKQ: An unrecognized or unsupported model MUST reach the vendor and surface as a vendor error via the classifier (D4), never as a pre-flight rejection.
- R-O8D2-3Q2K: `agentkit` MUST export `type Options map[string]string`.
- R-VXRP-9M2U: `agentkit` MUST export `type OptionKind int` with the constants `OptionKindNumber`, `OptionKindInteger`, `OptionKindText`, `OptionKindTextList`, `OptionKindReasoning` declared in that `iota` order starting at 0.
- R-OASU-V9JY: `agentkit` MUST export `type OptionSpec struct { Name string; Kind OptionKind; Description string }` with exactly those fields.
- R-W07I-15K8: `agentkit` MUST export `type Settings struct { Options Options; ToolChoice ToolChoice }` with exactly those fields.
- R-OEGK-0KS1: Every `OptionSpec` returned by a shipped wire's `OptionSpecs()` MUST have a non-empty `Name` and a non-empty `Description`, no two MUST share a `Name`, the slice MUST be in ascending `Name` order, and mutating a returned slice MUST have no effect on a later call.
- R-W1FE-EXAX: `AnthropicMessagesWire()`, `GeminiGenerateContentWire()`, `ChatWire()`, and `OpenAIChatWire()` MUST each return from `OptionSpecs()` exactly the names `effort` (`OptionKindReasoning`), `max_output_tokens` (`OptionKindInteger`), `stop` (`OptionKindTextList`), `temperature` (`OptionKindNumber`), `thinking` (`OptionKindReasoning`), `thinking_budget` (`OptionKindReasoning`), `thinking_level` (`OptionKindReasoning`), and `top_p` (`OptionKindNumber`); and `ResponsesWire()` and `OpenAIResponsesWire()` MUST each return exactly that set without `stop`.
- R-W2NA-SP1M: An `Options` value MUST parse under its `OptionSpec.Kind` as follows, and `Send` MUST reject any other text with `ErrInvalidConfig`: `OptionKindNumber` accepts exactly the strings `strconv.ParseFloat(s, 64)` accepts that denote a finite value; `OptionKindInteger` accepts exactly the strings `strconv.ParseInt(s, 10, 64)` accepts; `OptionKindText` accepts any string; `OptionKindTextList` accepts exactly a JSON array whose every element is a JSON string; `OptionKindReasoning` accepts exactly the strings `ParseReasoning` accepts.
- R-W3V7-6GSB: `Send` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged, when a reasoning option's parsed `Mode` does not fit its term: `effort` and `thinking_level` admit `ReasoningEffort`, `ReasoningOff`, and `ReasoningDefault`; `thinking_budget` admits `ReasoningBudget`, `ReasoningOff`, and `ReasoningDefault`; `thinking` admits `ReasoningOn`, `ReasoningOff`, and `ReasoningDefault`.
- R-W533-K8J0: `Send` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged, when `Settings.Options` holds more than one of `effort`, `thinking_level`, `thinking_budget`, and `thinking`, and the error message MUST name the conflicting keys.
- R-W6AZ-Y09P: The reasoning shape a wire renders and validates for a `Send` MUST be `ParseReasoning(v)` where `v` is the value of the one reasoning option in `Settings.Options`, and MUST be `ReasoningConfig{Mode: ReasoningDefault}` when `Settings.Options` holds no reasoning option.
- R-OI49-5W04: `Send` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged, when `Settings.Options` holds a key that is not the `Name` of any `OptionSpec` the conversation's wire returns from `OptionSpecs()`, and the error message MUST name the offending key.
- R-OJC5-JNQT: `Send` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged, when an `Options` value fails its `OptionSpec.Kind` grammar, and the error message MUST name the offending key.
- R-OKK1-XFHI: The Anthropic Messages wire MUST render `temperature` as top-level `temperature` (a JSON number), `top_p` as `top_p` (a JSON number), `max_output_tokens` as `max_tokens` (a JSON integer), and `stop` as `stop_sequences` (a JSON array of strings), pinned by a golden fixture.
- R-OLRY-B787: The Gemini GenerateContent wire MUST render `temperature` as `generationConfig.temperature` (a JSON number), `top_p` as `generationConfig.topP` (a JSON number), `max_output_tokens` as `generationConfig.maxOutputTokens` (a JSON integer), and `stop` as `generationConfig.stopSequences` (a JSON array of strings), pinned by a golden fixture.
- R-OMZU-OYYW: The OpenAI Chat wire and `ChatWire()` MUST render `temperature` as top-level `temperature` (a JSON number), `top_p` as `top_p` (a JSON number), `max_output_tokens` as `max_completion_tokens` (a JSON integer), and `stop` as `stop` (a JSON array of strings), pinned by a golden fixture.
- R-OO7R-2QPL: The OpenAI Responses wire and `ResponsesWire()` MUST render `temperature` as top-level `temperature` (a JSON number), `top_p` as `top_p` (a JSON number), and `max_output_tokens` as `max_output_tokens` (a JSON integer), pinned by a golden fixture.
- R-OPFN-GIGA: An option absent from `Settings.Options` MUST produce no corresponding field in the request body, and a request built from a nil or empty `Options` MUST be byte-identical to one built before this design added options.
- R-ZWKG-EPXR: `agentkit` MUST export `type ReasoningMode int` with the constants `ReasoningDefault`, `ReasoningOff`, `ReasoningOn`, `ReasoningEffort`, `ReasoningBudget` declared in that `iota` order starting at 0.
- R-ZXSC-SHOG: `agentkit` MUST export `type ReasoningConfig struct { Mode ReasoningMode; Effort Effort; Budget int }` with exactly those three fields.
- R-NU3H-L95J: `agentkit` MUST export `type Effort int` with the constants `EffortNone`, `EffortMinimal`, `EffortLow`, `EffortMedium`, `EffortHigh`, `EffortXHigh`, `EffortMax` declared in that `iota` order starting at 0.
- R-OQNJ-UA6Z: `agentkit` MUST export `func ParseReasoning(s string) (ReasoningConfig, error)`, `func (c ReasoningConfig) String() string`, and `func (e Effort) String() string`.
- R-ORVG-81XO: `ParseReasoning` MUST return `ReasoningConfig{Mode: ReasoningDefault}` for `"dynamic"`, `ReasoningConfig{Mode: ReasoningOff}` for `"off"`, `ReasoningConfig{Mode: ReasoningOn}` for `"on"`, `ReasoningConfig{Mode: ReasoningEffort, Effort: e}` for each of the seven `Effort` names `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and `ReasoningConfig{Mode: ReasoningBudget, Budget: n}` for a string `strconv.Atoi` parses to a non-negative `n`; for any other string, including the empty string, surrounding whitespace, and differing case, it MUST return a non-nil error whose message contains the input.
- R-OT3C-LTOD: `Effort.String()` MUST return exactly `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` for the seven constants in order, and `ReasoningConfig.String()` MUST satisfy `ParseReasoning(c.String()) == c` for every `c` that `ParseReasoning` can return.
- R-W7IW-BS0E: `Send` MUST accept `thinking_budget=0` on every wire that expresses `ReasoningBudget` and render the zero budget verbatim, so a catalog `MinBudget` of 0 (D21) is a sendable value.
- R-NVBD-Z0W8: A wire that renders `ReasoningEffort` MUST emit the level's lowercase constant name (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`) verbatim for every `Effort` constant, with no per-level rejection or substitution.
- R-NXR6-QKDM: The Anthropic Messages wire MUST express `ReasoningEffort` through the Messages API's effort control, in addition to the off and budget shapes it already expresses.
- R-P0EQ-WG4J: `ChatWire()` MUST express `ReasoningOn` as the top-level request field `reasoning` equal to `{"enabled":true}` and `ReasoningBudget` with budget N as `reasoning` equal to `{"max_tokens":N}`, MUST express `ReasoningOff` as `reasoning_effort` `"none"` and `ReasoningEffort` as `reasoning_effort` with the level name, and MUST emit no `reasoning` field for any other mode; pinned by a golden fixture.
- R-P1MN-A7V8: `ResponsesWire()` MUST express `ReasoningOn` as the top-level request field `reasoning` equal to `{"enabled":true}` and `ReasoningBudget` with budget N as `reasoning` equal to `{"max_tokens":N}`, MUST express `ReasoningOff` as `reasoning` equal to `{"effort":"none"}` and `ReasoningEffort` as `reasoning` equal to `{"effort":<level name>}`, and MUST emit no `reasoning` field for `ReasoningDefault`; pinned by a golden fixture.
- R-P2UJ-NZLX: `OpenAIChatWire()` and `OpenAIResponsesWire()` MUST fail `Send` with `ErrInvalidConfig`, making no provider call, for `ReasoningOn` and for `ReasoningBudget`, while `ChatWire()` and `ResponsesWire()` MUST accept both.
- R-0085-K15U: `agentkit` MUST export `type ToolChoice struct { Mode ToolChoiceMode; Name string }` with exactly those two fields.
- R-01G1-XSWJ: `agentkit` MUST export `type ToolChoiceMode int` with the constants `ToolChoiceAuto`, `ToolChoiceNone`, `ToolChoiceRequired`, `ToolChoiceTool` declared in that `iota` order starting at 0.
