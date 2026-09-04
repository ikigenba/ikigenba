package agentkit

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOptionsBehavesAsStringMap(t *testing.T) {
	// R-O8D2-3Q2K: Options is the user's string-to-string option vocabulary.
	options := Options{"temperature": "0.7"}
	options["max_output_tokens"] = "4096"

	if got := options["temperature"]; got != "0.7" {
		t.Fatalf("temperature = %q, want %q", got, "0.7")
	}
	if got := options["max_output_tokens"]; got != "4096" {
		t.Fatalf("max_output_tokens = %q, want %q", got, "4096")
	}
}

func TestOptionKindValuesFollowDeclaredOrder(t *testing.T) {
	// R-VXRP-9M2U: OptionKind constants use this exact iota order from zero.
	tests := []struct {
		name string
		kind OptionKind
		want int
	}{
		{name: "number", kind: OptionKindNumber, want: 0},
		{name: "integer", kind: OptionKindInteger, want: 1},
		{name: "text", kind: OptionKindText, want: 2},
		{name: "text list", kind: OptionKindTextList, want: 3},
		{name: "reasoning", kind: OptionKindReasoning, want: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := int(test.kind); got != test.want {
				t.Fatalf("OptionKind value = %d, want %d", got, test.want)
			}
		})
	}
}

func TestOptionSpecHasDocumentedFields(t *testing.T) {
	// R-OASU-V9JY: OptionSpec has exactly Name, Kind, and Description.
	spec := OptionSpec{
		Name:        "temperature",
		Kind:        OptionKindNumber,
		Description: "sampling temperature",
	}

	if spec.Name != "temperature" || spec.Kind != OptionKindNumber || spec.Description != "sampling temperature" {
		t.Fatalf("OptionSpec fields did not preserve their values: %#v", spec)
	}
	if got := reflect.TypeOf(spec).NumField(); got != 3 {
		t.Fatalf("OptionSpec field count = %d, want 3", got)
	}
}

func TestValidateOptionValueFollowsKindGrammar(t *testing.T) {
	// R-W2NA-SP1M
	tests := []struct {
		name   string
		kind   OptionKind
		accept []string
		reject []string
	}{
		{name: "number", kind: OptionKindNumber, accept: []string{"0", "0.7", "-3", "2e-1"}, reject: []string{"", "abc", "NaN", "Inf", "+Inf", "-Inf"}},
		{name: "integer", kind: OptionKindInteger, accept: []string{"4096", "-1", "0"}, reject: []string{"", "4.5", "abc"}},
		{name: "text", kind: OptionKindText, accept: []string{"", "anything at all"}},
		{name: "text list", kind: OptionKindTextList, accept: []string{`[]`, `["END"]`, `["END","STOP"]`}, reject: []string{"", `not json`, `["END",1]`, `{"a":1}`, `"END"`}},
		{name: "reasoning", kind: OptionKindReasoning, accept: []string{"dynamic", "off", "on", "none", "minimal", "low", "medium", "high", "xhigh", "max", "0", "8192"}, reject: []string{"", "OFF", "-1", "not-reasoning"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.accept {
				if err := validateOptionValue(test.kind, value); err != nil {
					t.Errorf("validateOptionValue(%d, %q) = %v, want nil", test.kind, value, err)
				}
			}
			for _, value := range test.reject {
				if err := validateOptionValue(test.kind, value); err == nil {
					t.Errorf("validateOptionValue(%d, %q) = nil, want error", test.kind, value)
				}
			}
		})
	}
}

func TestValidateOptionsChecksKeyLegalityAndGrammar(t *testing.T) {
	// R-OI49-5W04
	// R-OJC5-JNQT
	specs := []OptionSpec{
		{Name: "temperature", Kind: OptionKindNumber},
		{Name: "stop", Kind: OptionKindTextList},
	}
	for _, options := range []Options{nil, {}, {"temperature": "0.7", "stop": `["END"]`}} {
		if err := validateOptions(options, specs); err != nil {
			t.Errorf("validateOptions(%#v) = %v, want nil", options, err)
		}
	}

	tests := []struct {
		name    string
		options Options
		wantKey string
	}{
		{name: "unknown key", options: Options{"unknown_key": "x"}, wantKey: "unknown_key"},
		{name: "invalid grammar", options: Options{"temperature": "not-a-number"}, wantKey: "temperature"},
		{name: "sorted keys", options: Options{"unknown_b": "x", "unknown_a": "y"}, wantKey: "unknown_a"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOptions(test.options, specs)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("validateOptions() error = %v, want ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), test.wantKey) {
				t.Fatalf("validateOptions() error = %v, want offending key %q", err, test.wantKey)
			}
		})
	}
}

func TestSettingsHasExactWireNeutralShapeAndZeroValue(t *testing.T) {
	// R-W07I-15K8
	// R-O755-PYBV
	typeOfSettings := reflect.TypeFor[Settings]()
	if got := typeOfSettings.NumField(); got != 2 {
		t.Fatalf("Settings field count = %d, want 2", got)
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Options", typeOf: reflect.TypeFor[Options]()},
		{name: "ToolChoice", typeOf: reflect.TypeFor[ToolChoice]()},
	}
	for index, want := range wantFields {
		field := typeOfSettings.Field(index)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Fatalf("Settings field %d = %s %v, want %s %v", index, field.Name, field.Type, want.name, want.typeOf)
		}
	}
	if !settingsAreZero(Settings{}) || !reflect.DeepEqual(cloneSettings(Settings{}), Settings{}) {
		t.Fatal("zero Settings does not preserve vendor defaults with no options")
	}
}

func settingsForReasoning(config ReasoningConfig) Settings {
	if config.Mode == ReasoningDefault {
		return Settings{}
	}
	key := map[ReasoningMode]string{
		ReasoningOff:    "effort",
		ReasoningOn:     "thinking",
		ReasoningEffort: "effort",
		ReasoningBudget: "thinking_budget",
	}[config.Mode]
	return Settings{Options: Options{key: config.String()}}
}

func TestReasoningConfigExpressesEveryNeutralShape(t *testing.T) {
	// R-3QUG-OHV9
	configs := []ReasoningConfig{
		{Mode: ReasoningDefault},
		{Mode: ReasoningOff},
		{Mode: ReasoningOn},
		{Mode: ReasoningEffort, Effort: EffortNone},
		{Mode: ReasoningEffort, Effort: EffortMinimal},
		{Mode: ReasoningEffort, Effort: EffortLow},
		{Mode: ReasoningEffort, Effort: EffortMedium},
		{Mode: ReasoningEffort, Effort: EffortHigh},
		{Mode: ReasoningEffort, Effort: EffortXHigh},
		{Mode: ReasoningEffort, Effort: EffortMax},
		{Mode: ReasoningBudget, Budget: 8000},
	}
	capabilities := wireCapabilities{
		name:      "test grammar",
		reasoning: reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
	}
	seen := make(map[ReasoningMode]bool)
	for _, config := range configs {
		if err := capabilities.validate(settingsForReasoning(config)); err != nil {
			t.Errorf("consumer-built neutral config %#v was rejected: %v", config, err)
		}
		seen[config.Mode] = true
	}
	wantModes := map[ReasoningMode]bool{
		ReasoningDefault: true,
		ReasoningOff:     true,
		ReasoningOn:      true,
		ReasoningEffort:  true,
		ReasoningBudget:  true,
	}
	if !reflect.DeepEqual(seen, wantModes) {
		t.Fatalf("consumer-expressible reasoning modes = %v, want %v", seen, wantModes)
	}
	for _, effort := range []Effort{-1, EffortMax + 1} {
		if err := validateReasoningConfig(ReasoningConfig{Mode: ReasoningEffort, Effort: effort}); err == nil {
			t.Errorf("out-of-range effort %d was accepted", effort)
		}
	}
}

func TestParseReasoningGrammar(t *testing.T) {
	// R-OQNJ-UA6Z: the reasoning parser and String methods are exported with the specified signatures.
	var parse func(string) (ReasoningConfig, error)
	var configString func(ReasoningConfig) string
	var effortString func(Effort) string
	parse = ParseReasoning
	configString = ReasoningConfig.String
	effortString = Effort.String
	if parse == nil || configString == nil || effortString == nil {
		t.Fatal("reasoning API contains a nil function")
	}

	// R-ORVG-81XO: every exact token maps to its neutral reasoning shape.
	tests := []struct {
		input string
		want  ReasoningConfig
	}{
		{input: "dynamic", want: ReasoningConfig{Mode: ReasoningDefault}},
		{input: "off", want: ReasoningConfig{Mode: ReasoningOff}},
		{input: "on", want: ReasoningConfig{Mode: ReasoningOn}},
		{input: "none", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortNone}},
		{input: "minimal", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMinimal}},
		{input: "low", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortLow}},
		{input: "medium", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}},
		{input: "high", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh}},
		{input: "xhigh", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortXHigh}},
		{input: "max", want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMax}},
		{input: "8192", want: ReasoningConfig{Mode: ReasoningBudget, Budget: 8192}},
		{input: "0", want: ReasoningConfig{Mode: ReasoningBudget, Budget: 0}},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseReasoning(test.input)
			if err != nil {
				t.Fatalf("ParseReasoning(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseReasoning(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}

	for _, input := range []string{"", " off ", "OFF", "-1", "not-reasoning"} {
		t.Run("invalid_"+input, func(t *testing.T) {
			_, err := ParseReasoning(input)
			if err == nil {
				t.Fatalf("ParseReasoning(%q) returned nil error", input)
			}
			if quotedInput := strconv.Quote(input); !strings.Contains(err.Error(), quotedInput) {
				t.Fatalf("ParseReasoning(%q) error %q does not contain quoted input %q", input, err, quotedInput)
			}
		})
	}
}

func TestReasoningTermMustFitItsMode(t *testing.T) {
	// R-W3V7-6GSB
	accept := []struct {
		name    string
		options Options
		want    ReasoningConfig
	}{
		{name: "effort level", options: Options{"effort": "high"}, want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh}},
		{name: "effort off", options: Options{"effort": "off"}, want: ReasoningConfig{Mode: ReasoningOff}},
		{name: "effort dynamic", options: Options{"effort": "dynamic"}, want: ReasoningConfig{Mode: ReasoningDefault}},
		{name: "thinking level", options: Options{"thinking_level": "low"}, want: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortLow}},
		{name: "thinking budget", options: Options{"thinking_budget": "4096"}, want: ReasoningConfig{Mode: ReasoningBudget, Budget: 4096}},
		{name: "thinking budget off", options: Options{"thinking_budget": "off"}, want: ReasoningConfig{Mode: ReasoningOff}},
		{name: "thinking on", options: Options{"thinking": "on"}, want: ReasoningConfig{Mode: ReasoningOn}},
		{name: "thinking off", options: Options{"thinking": "off"}, want: ReasoningConfig{Mode: ReasoningOff}},
		{name: "nil options", options: nil, want: ReasoningConfig{Mode: ReasoningDefault}},
		{name: "empty options", options: Options{}, want: ReasoningConfig{Mode: ReasoningDefault}},
	}
	for _, test := range accept {
		t.Run("accept "+test.name, func(t *testing.T) {
			got, err := resolveReasoning(test.options)
			if err != nil {
				t.Fatalf("resolveReasoning(%#v) error = %v, want nil", test.options, err)
			}
			if got != test.want {
				t.Fatalf("resolveReasoning(%#v) = %#v, want %#v", test.options, got, test.want)
			}
		})
	}

	reject := []struct {
		key   string
		value string
		want  string
	}{
		{key: "effort", value: "on", want: `option "effort" does not admit mode on`},
		{key: "thinking_level", value: "8192", want: `option "thinking_level" does not admit mode budget`},
		{key: "thinking_budget", value: "on", want: `option "thinking_budget" does not admit mode on`},
		{key: "thinking", value: "high", want: `option "thinking" does not admit mode effort`},
	}
	for _, test := range reject {
		t.Run("reject "+test.key, func(t *testing.T) {
			_, err := resolveReasoning(Options{test.key: test.value})
			if err == nil {
				t.Fatalf("resolveReasoning(%q=%q) error = nil, want term mismatch", test.key, test.value)
			}
			if err.Error() != test.want {
				t.Fatalf("resolveReasoning(%q=%q) error = %q, want %q", test.key, test.value, err, test.want)
			}
		})
	}
}

func TestConflictingReasoningOptionsFail(t *testing.T) {
	// R-W533-K8J0
	tests := []struct {
		options Options
		want    string
	}{
		{options: Options{"effort": "high", "thinking": "on"}, want: "conflicting reasoning options: effort, thinking"},
		{options: Options{"effort": "high", "thinking_level": "low", "thinking_budget": "4096"}, want: "conflicting reasoning options: effort, thinking_budget, thinking_level"},
		{options: Options{"effort": "high", "thinking_level": "low", "thinking_budget": "4096", "thinking": "on"}, want: "conflicting reasoning options: effort, thinking, thinking_budget, thinking_level"},
	}
	for _, test := range tests {
		_, err := resolveReasoning(test.options)
		if err == nil {
			t.Fatalf("resolveReasoning(%#v) error = nil, want conflict", test.options)
		}
		if err.Error() != test.want {
			t.Errorf("resolveReasoning(%#v) error = %q, want %q", test.options, err, test.want)
		}
	}

	_, firstErr := resolveReasoning(Options{"thinking": "on", "effort": "high"})
	_, secondErr := resolveReasoning(Options{"effort": "high", "thinking": "on"})
	if firstErr == nil || secondErr == nil || firstErr.Error() != secondErr.Error() {
		t.Fatalf("conflict errors are not deterministic: first=%v second=%v", firstErr, secondErr)
	}
}

func TestSettingsReasoningResolvesTheOnePresentOption(t *testing.T) {
	// R-W6AZ-Y09P
	// R-W7IW-BS0E
	tests := []struct {
		settings Settings
		want     ReasoningConfig
	}{
		{settings: Settings{}, want: ReasoningConfig{Mode: ReasoningDefault}},
		{settings: Settings{Options: Options{}}, want: ReasoningConfig{Mode: ReasoningDefault}},
		{settings: Settings{Options: Options{"thinking": "on"}}, want: ReasoningConfig{Mode: ReasoningOn}},
		{settings: Settings{Options: Options{"thinking_budget": "0"}}, want: ReasoningConfig{Mode: ReasoningBudget, Budget: 0}},
	}
	for _, test := range tests {
		if got := settingsReasoning(test.settings); got != test.want {
			t.Errorf("settingsReasoning(%#v) = %#v, want %#v", test.settings, got, test.want)
		}
	}
}

func TestReasoningStringsAndRoundTrip(t *testing.T) {
	// R-OT3C-LTOD: effort names are exact and every parseable shape round-trips.
	efforts := []struct {
		effort Effort
		want   string
	}{
		{effort: EffortNone, want: "none"},
		{effort: EffortMinimal, want: "minimal"},
		{effort: EffortLow, want: "low"},
		{effort: EffortMedium, want: "medium"},
		{effort: EffortHigh, want: "high"},
		{effort: EffortXHigh, want: "xhigh"},
		{effort: EffortMax, want: "max"},
	}
	configs := []ReasoningConfig{
		{Mode: ReasoningDefault},
		{Mode: ReasoningOff},
		{Mode: ReasoningOn},
	}
	for _, test := range efforts {
		if got := test.effort.String(); got != test.want {
			t.Errorf("Effort(%d).String() = %q, want %q", test.effort, got, test.want)
		}
		configs = append(configs, ReasoningConfig{Mode: ReasoningEffort, Effort: test.effort})
	}
	configs = append(configs,
		ReasoningConfig{Mode: ReasoningBudget, Budget: 0},
		ReasoningConfig{Mode: ReasoningBudget, Budget: 8192},
	)

	for _, config := range configs {
		got, err := ParseReasoning(config.String())
		if err != nil {
			t.Errorf("ParseReasoning(%#v.String()) error = %v", config, err)
			continue
		}
		if got != config {
			t.Errorf("ParseReasoning(%#v.String()) = %#v", config, got)
		}
	}
}

func TestAmbiguousNeutralSettingsAreInvalid(t *testing.T) {
	capabilities := wireCapabilities{
		name:       "complete grammar",
		reasoning:  reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
		toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
	}
	for _, config := range []ReasoningConfig{
		{Mode: ReasoningDefault, Effort: EffortLow},
		{Mode: ReasoningEffort, Effort: EffortHigh, Budget: 1},
		{Mode: ReasoningBudget, Budget: -1},
	} {
		if err := validateReasoningConfig(config); err == nil {
			t.Errorf("ambiguous reasoning accepted: %#v", config)
		}
	}
	tests := []Settings{
		{ToolChoice: ToolChoice{Mode: ToolChoiceAuto, Name: "lookup"}},
		{ToolChoice: ToolChoice{Mode: ToolChoiceTool}},
	}
	for _, settings := range tests {
		if err := capabilities.validate(settings); err == nil {
			t.Errorf("ambiguous settings accepted: %#v", settings)
		}
	}
}
