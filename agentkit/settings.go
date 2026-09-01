package agentkit

import "fmt"

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

type reasoningShapes uint8

const (
	reasoningShapeOff reasoningShapes = 1 << iota
	reasoningShapeOn
	reasoningShapeEffort
	reasoningShapeBudget
)

type toolChoiceShapes uint8

const (
	toolChoiceShapeNone toolChoiceShapes = 1 << iota
	toolChoiceShapeRequired
	toolChoiceShapeTool
)

type wireCapabilities struct {
	name       string
	reasoning  reasoningShapes
	toolChoice toolChoiceShapes
}

func (capabilities wireCapabilities) validate(settings Settings) error {
	if err := validateReasoningConfig(settings.Reasoning); err != nil {
		return fmt.Errorf("%w: %s reasoning: %w", ErrInvalidConfig, capabilities.name, err)
	}
	if err := validateToolChoice(settings.ToolChoice); err != nil {
		return fmt.Errorf("%w: %s tool choice: %w", ErrInvalidConfig, capabilities.name, err)
	}

	reasoningShape := shapeForReasoning(settings.Reasoning.Mode)
	if reasoningShape != 0 && capabilities.reasoning&reasoningShape == 0 {
		return fmt.Errorf("%w: %s wire cannot express reasoning mode %s", ErrInvalidConfig, capabilities.name, reasoningModeName(settings.Reasoning.Mode))
	}
	toolChoiceShape := shapeForToolChoice(settings.ToolChoice.Mode)
	if toolChoiceShape != 0 && capabilities.toolChoice&toolChoiceShape == 0 {
		return fmt.Errorf("%w: %s wire cannot express tool choice %s", ErrInvalidConfig, capabilities.name, toolChoiceModeName(settings.ToolChoice.Mode))
	}
	return nil
}

func validateReasoningConfig(config ReasoningConfig) error {
	switch config.Mode {
	case ReasoningDefault, ReasoningOff, ReasoningOn:
		if config.Effort != EffortNone || config.Budget != 0 {
			return fmt.Errorf("mode %s cannot carry effort or budget", reasoningModeName(config.Mode))
		}
	case ReasoningEffort:
		if config.Effort < EffortLow || config.Effort > EffortHigh {
			return fmt.Errorf("effort mode requires low, medium, or high effort")
		}
		if config.Budget != 0 {
			return fmt.Errorf("effort mode cannot carry a budget")
		}
	case ReasoningBudget:
		if config.Budget <= 0 {
			return fmt.Errorf("budget mode requires a positive budget")
		}
		if config.Effort != EffortNone {
			return fmt.Errorf("budget mode cannot carry an effort")
		}
	default:
		return fmt.Errorf("unknown mode %d", config.Mode)
	}
	return nil
}

func validateToolChoice(choice ToolChoice) error {
	switch choice.Mode {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		if choice.Name != "" {
			return fmt.Errorf("mode %s cannot name a tool", toolChoiceModeName(choice.Mode))
		}
	case ToolChoiceTool:
		if choice.Name == "" {
			return fmt.Errorf("named-tool mode requires a tool name")
		}
	default:
		return fmt.Errorf("unknown mode %d", choice.Mode)
	}
	return nil
}

func shapeForReasoning(mode ReasoningMode) reasoningShapes {
	switch mode {
	case ReasoningOff:
		return reasoningShapeOff
	case ReasoningOn:
		return reasoningShapeOn
	case ReasoningEffort:
		return reasoningShapeEffort
	case ReasoningBudget:
		return reasoningShapeBudget
	default:
		return 0
	}
}

func shapeForToolChoice(mode ToolChoiceMode) toolChoiceShapes {
	switch mode {
	case ToolChoiceNone:
		return toolChoiceShapeNone
	case ToolChoiceRequired:
		return toolChoiceShapeRequired
	case ToolChoiceTool:
		return toolChoiceShapeTool
	default:
		return 0
	}
}

func reasoningModeName(mode ReasoningMode) string {
	switch mode {
	case ReasoningDefault:
		return "default"
	case ReasoningOff:
		return "off"
	case ReasoningOn:
		return "on"
	case ReasoningEffort:
		return "effort"
	case ReasoningBudget:
		return "budget"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

func toolChoiceModeName(mode ToolChoiceMode) string {
	switch mode {
	case ToolChoiceAuto:
		return "auto"
	case ToolChoiceNone:
		return "none"
	case ToolChoiceRequired:
		return "required"
	case ToolChoiceTool:
		return "tool"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

func effortName(effort Effort) string {
	switch effort {
	case EffortLow:
		return "low"
	case EffortMedium:
		return "medium"
	case EffortHigh:
		return "high"
	default:
		return ""
	}
}

func cloneSettings(settings Settings) Settings {
	clone := settings
	if settings.Temperature != nil {
		value := *settings.Temperature
		clone.Temperature = &value
	}
	if settings.TopP != nil {
		value := *settings.TopP
		clone.TopP = &value
	}
	if settings.MaxOutputTokens != nil {
		value := *settings.MaxOutputTokens
		clone.MaxOutputTokens = &value
	}
	clone.StopSequences = append([]string(nil), settings.StopSequences...)
	return clone
}

func settingsAreZero(settings Settings) bool {
	return settings.Temperature == nil &&
		settings.TopP == nil &&
		settings.MaxOutputTokens == nil &&
		len(settings.StopSequences) == 0 &&
		settings.ToolChoice == (ToolChoice{}) &&
		settings.Reasoning == (ReasoningConfig{})
}
