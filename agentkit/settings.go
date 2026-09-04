package agentkit

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

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

// Supported option value shapes.
const (
	OptionKindNumber    OptionKind = iota // a finite decimal number: "0.7", "1", "2e-1"
	OptionKindInteger                     // a base-10 integer: "4096", "-1"
	OptionKindText                        // any string, passed through verbatim
	OptionKindTextList                    // a JSON array of strings: `["END","STOP"]`
	OptionKindReasoning                   // a reasoning choice: "high", "off", "on", "8192", "dynamic"
)

// OptionSpec describes one option a wire accepts: the wire-neutral name a
// consumer uses as the Options key, the kind that fixes the value grammar, and
// a one-line description suitable for help text.
type OptionSpec struct {
	Name        string
	Kind        OptionKind
	Description string
}

// validateOptions checks Settings.Options against the wire's option
// vocabulary: every key must name a known OptionSpec (R-OI49-5W04) and its
// value must parse under that spec's Kind grammar (R-OJC5-JNQT, whose
// grammar per kind is R-W2NA-SP1M). Keys are checked in sorted order so the
// reported key is deterministic when more than one is invalid. A nil or
// empty Options is always valid.
func validateOptions(options Options, specs []OptionSpec) error {
	if len(options) == 0 {
		return nil
	}
	byName := make(map[string]OptionSpec, len(specs))
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		spec, ok := byName[key]
		if !ok {
			return fmt.Errorf("%w: unknown option %q", ErrInvalidConfig, key)
		}
		if err := validateOptionValue(spec.Kind, options[key]); err != nil {
			return fmt.Errorf("%w: option %q: %w", ErrInvalidConfig, key, err)
		}
	}
	return nil
}

// validateOptionValue parses value under kind's grammar (R-W2NA-SP1M).
func validateOptionValue(kind OptionKind, value string) error {
	switch kind {
	case OptionKindNumber:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("invalid number %q", value)
		}
	case OptionKindInteger:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("invalid integer %q", value)
		}
	case OptionKindText:
		// any string is accepted verbatim
	case OptionKindTextList:
		var elements []any
		if err := json.Unmarshal([]byte(value), &elements); err != nil {
			return fmt.Errorf("invalid text list %q", value)
		}
		for _, element := range elements {
			if _, ok := element.(string); !ok {
				return fmt.Errorf("invalid text list %q: element is not a string", value)
			}
		}
	case OptionKindReasoning:
		if _, err := ParseReasoning(value); err != nil {
			return fmt.Errorf("invalid reasoning value %q", value)
		}
	default:
		return fmt.Errorf("unknown option kind %d", kind)
	}
	return nil
}

// Settings are the generation controls for a turn. A zero Settings requests
// the vendor's own defaults for everything and sends no option. Settings
// carries no vendor vocabulary; each wire renders what it can express and
// fails loud on what it cannot.
type Settings struct {
	// Options are the user's string options, including the reasoning choice.
	Options Options
	// ToolChoice directs tool selection for the turn (see ToolChoice).
	ToolChoice ToolChoice
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
	ReasoningEffort                       // reasoning effort level
	ReasoningBudget                       // an explicit token budget
)

// ReasoningConfig is a neutral reasoning request. Mode selects the shape; Effort
// is read only when Mode is ReasoningEffort and Budget only when Mode is
// ReasoningBudget. Under ReasoningEffort, Effort's zero value EffortNone is the
// literal "none" level some vendors accept, not an absent value. A Mode a wire cannot express fails at Send. It is named
// ReasoningConfig, not Reasoning, because the D2 block variant owns the exported
// name Reasoning in package agentkit; the Settings field keeps the name Reasoning.
type ReasoningConfig struct {
	Mode   ReasoningMode
	Effort Effort // none | minimal | low | medium | high | xhigh | max
	Budget int    // token budget, when Mode is ReasoningBudget
}

// Effort is the neutral reasoning effort level, read only when a ReasoningConfig's Mode
// is ReasoningEffort. The set is exhaustive over every level any cataloged offering
// accepts (D21); a wire renders the level's lowercase name verbatim, and whether a given
// model honors it is the vendor's judgment, not agentkit's.
type Effort int

// Neutral reasoning effort levels.
const (
	EffortNone Effort = iota // "none" — the vendor's no-reasoning effort level
	EffortMinimal
	EffortLow
	EffortMedium
	EffortHigh
	EffortXHigh
	EffortMax
)

// ParseReasoning parses the value grammar of an OptionKindReasoning option.
func ParseReasoning(s string) (ReasoningConfig, error) {
	switch s {
	case "dynamic":
		return ReasoningConfig{Mode: ReasoningDefault}, nil
	case "off":
		return ReasoningConfig{Mode: ReasoningOff}, nil
	case "on":
		return ReasoningConfig{Mode: ReasoningOn}, nil
	case "none":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortNone}, nil
	case "minimal":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMinimal}, nil
	case "low":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortLow}, nil
	case "medium":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}, nil
	case "high":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh}, nil
	case "xhigh":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortXHigh}, nil
	case "max":
		return ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMax}, nil
	}

	if budget, err := strconv.Atoi(s); err == nil && budget >= 0 {
		return ReasoningConfig{Mode: ReasoningBudget, Budget: budget}, nil
	}
	return ReasoningConfig{}, fmt.Errorf("invalid reasoning value %q", s)
}

// String returns the value as ParseReasoning's canonical input grammar.
func (c ReasoningConfig) String() string {
	switch c.Mode {
	case ReasoningDefault:
		return "dynamic"
	case ReasoningOff:
		return "off"
	case ReasoningOn:
		return "on"
	case ReasoningEffort:
		return c.Effort.String()
	case ReasoningBudget:
		return strconv.Itoa(c.Budget)
	default:
		return ""
	}
}

// String returns the effort level's lowercase name.
func (e Effort) String() string {
	return effortName(e)
}

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

// reasoningOptionKeys are the four reasoning term keys a catalog offering may
// use (D21); Settings.Options may carry at most one of them (R-W533-K8J0).
var reasoningOptionKeys = []string{"effort", "thinking_level", "thinking_budget", "thinking"}

// reasoningTermAdmits reports whether mode is an admissible ParseReasoning
// result for the reasoning term key (R-W3V7-6GSB).
func reasoningTermAdmits(key string, mode ReasoningMode) bool {
	switch key {
	case "effort", "thinking_level":
		return mode == ReasoningEffort || mode == ReasoningOff || mode == ReasoningDefault
	case "thinking_budget":
		return mode == ReasoningBudget || mode == ReasoningOff || mode == ReasoningDefault
	case "thinking":
		return mode == ReasoningOn || mode == ReasoningOff || mode == ReasoningDefault
	default:
		return false
	}
}

// resolveReasoning is the R-W6AZ-Y09P reasoning shape a wire renders and
// validates for a Send: ParseReasoning(v) of the one reasoning option
// present in options, or ReasoningConfig{Mode: ReasoningDefault} when none
// is present. It fails when more than one reasoning key is present
// (R-W533-K8J0, naming the conflicting keys in sorted order for determinism)
// or when the parsed Mode does not fit its term (R-W3V7-6GSB). By the time
// this runs, validateOptions has already confirmed any present value parses
// under OptionKindReasoning's grammar, so the ParseReasoning call here is not
// expected to fail — but its error is still surfaced for defense in depth.
func resolveReasoning(options Options) (ReasoningConfig, error) {
	var present []string
	for _, key := range reasoningOptionKeys {
		if _, ok := options[key]; ok {
			present = append(present, key)
		}
	}
	if len(present) > 1 {
		sort.Strings(present)
		return ReasoningConfig{}, fmt.Errorf("conflicting reasoning options: %s", strings.Join(present, ", "))
	}
	if len(present) == 0 {
		return ReasoningConfig{Mode: ReasoningDefault}, nil
	}
	key := present[0]
	config, err := ParseReasoning(options[key])
	if err != nil {
		return ReasoningConfig{}, fmt.Errorf("option %q: %w", key, err)
	}
	if !reasoningTermAdmits(key, config.Mode) {
		return ReasoningConfig{}, fmt.Errorf("option %q does not admit mode %s", key, reasoningModeName(config.Mode))
	}
	return config, nil
}

// settingsReasoning returns the ReasoningConfig carried by Settings.Options
// (R-W6AZ-Y09P), ignoring a resolveReasoning error: every render call site
// runs only after wireCodec.validateSettings has already called
// resolveReasoning (via wireCapabilities.validate) and returned nil, so the
// error branch here is unreachable in practice.
func settingsReasoning(settings Settings) ReasoningConfig {
	config, _ := resolveReasoning(settings.Options)
	return config
}

// settingsFloatOption returns the parsed float64 value of options[key] and
// true, or (0, false) if key is absent. It is called only after
// validateOptions has confirmed any present value parses under
// OptionKindNumber, so the ignored parse error mirrors settingsReasoning's
// defense-in-depth comment: unreachable in practice, but harmless if it were
// ever reached (R-OKK1-XFHI, R-OLRY-B787, R-OMZU-OYYW, R-OO7R-2QPL).
func settingsFloatOption(options Options, key string) (float64, bool) {
	value, ok := options[key]
	if !ok {
		return 0, false
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed, true
}

// settingsMaxOutputTokens returns the parsed int value of
// options["max_output_tokens"] and true, or (0, false) if the key is absent.
// See settingsFloatOption's comment on the ignored parse error.
func settingsMaxOutputTokens(options Options) (int, bool) {
	value, ok := options["max_output_tokens"]
	if !ok {
		return 0, false
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return int(parsed), true
}

// settingsStopSequences returns the parsed []string value of options["stop"]
// and true, or (nil, false) if the key is absent. See settingsFloatOption's
// comment on the ignored parse error.
func settingsStopSequences(options Options) ([]string, bool) {
	value, ok := options["stop"]
	if !ok {
		return nil, false
	}
	var elements []string
	_ = json.Unmarshal([]byte(value), &elements)
	return elements, true
}

func (capabilities wireCapabilities) validate(settings Settings) error {
	reasoning, err := resolveReasoning(settings.Options)
	if err != nil {
		return fmt.Errorf("%w: %s reasoning: %w", ErrInvalidConfig, capabilities.name, err)
	}
	if err := validateReasoningConfig(reasoning); err != nil {
		return fmt.Errorf("%w: %s reasoning: %w", ErrInvalidConfig, capabilities.name, err)
	}
	if err := validateToolChoice(settings.ToolChoice); err != nil {
		return fmt.Errorf("%w: %s tool choice: %w", ErrInvalidConfig, capabilities.name, err)
	}

	reasoningShape := shapeForReasoning(reasoning.Mode)
	if reasoningShape != 0 && capabilities.reasoning&reasoningShape == 0 {
		return fmt.Errorf("%w: %s wire cannot express reasoning mode %s", ErrInvalidConfig, capabilities.name, reasoningModeName(reasoning.Mode))
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
		if config.Effort < EffortNone || config.Effort > EffortMax {
			return fmt.Errorf("effort mode requires a declared effort level")
		}
		if config.Budget != 0 {
			return fmt.Errorf("effort mode cannot carry a budget")
		}
	case ReasoningBudget:
		if config.Budget < 0 {
			return fmt.Errorf("budget mode requires a non-negative budget")
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
	case EffortNone:
		return "none"
	case EffortMinimal:
		return "minimal"
	case EffortLow:
		return "low"
	case EffortMedium:
		return "medium"
	case EffortHigh:
		return "high"
	case EffortXHigh:
		return "xhigh"
	case EffortMax:
		return "max"
	default:
		return ""
	}
}

func cloneSettings(settings Settings) Settings {
	clone := settings
	if settings.Options != nil {
		clone.Options = make(Options, len(settings.Options))
		for key, value := range settings.Options {
			clone.Options[key] = value
		}
	}
	return clone
}

func settingsAreZero(settings Settings) bool {
	return len(settings.Options) == 0 && settings.ToolChoice == (ToolChoice{})
}
