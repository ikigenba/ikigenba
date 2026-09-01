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
	// Reasoning requests generation behavior in a wire-neutral shape (see
	// ReasoningConfig); it is not transcript content.
	Reasoning ReasoningConfig
}

// ReasoningMode is the neutral reasoning request. A wire renders the shapes it
// supports and rejects the rest at Send (ErrInvalidConfig) — agentkit never
// silently rewrites "budget 8000" into "effort high" or drops reasoning to make
// a request fit.
type ReasoningMode int

// Neutral reasoning modes, from vendor default through an explicit budget.
const (
	ReasoningDefault ReasoningMode = iota // leave reasoning to the vendor default
	ReasoningOff                          // explicitly disable reasoning
	ReasoningOn                           // bare on, no parameter
	ReasoningEffort                       // low/medium/high effort level
	ReasoningBudget                       // an explicit token budget
)

// ReasoningConfig is a neutral reasoning request. Mode selects the shape; Effort
// is read only when Mode is ReasoningEffort and Budget only when Mode is
// ReasoningBudget. A Mode a wire cannot express fails at Send. ReasoningConfig
// describes a generation request; the exported Reasoning block instead describes
// transcript content. The Settings field uses the domain label Reasoning for its
// request value.
type ReasoningConfig struct {
	Mode   ReasoningMode
	Effort Effort // low | medium | high
	Budget int    // token budget, when Mode is ReasoningBudget
}

// Effort is the neutral reasoning effort level, read only when a ReasoningConfig's Mode
// is ReasoningEffort. The zero value EffortNone means "not an effort request".
type Effort int

// Neutral reasoning effort levels.
const (
	EffortNone Effort = iota // unset — not an effort request
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

// Neutral tool-selection modes.
const (
	ToolChoiceAuto     ToolChoiceMode = iota // model decides (default)
	ToolChoiceNone                           // no tool call this turn
	ToolChoiceRequired                       // must call some tool
	ToolChoiceTool                           // must call the tool named in ToolChoice.Name
)
