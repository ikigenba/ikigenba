package agentkit

import (
	"reflect"
	"testing"
)

func TestZeroSettingsRequestsDefaultsAndPointerZeroesRemainSet(t *testing.T) {
	// R-3PMK-AQ4K
	zero := Settings{}
	if !settingsAreZero(zero) {
		t.Fatalf("zero Settings was not recognized as vendor-default settings: %#v", zero)
	}
	if zero.Temperature != nil || zero.TopP != nil || zero.MaxOutputTokens != nil || len(zero.StopSequences) != 0 || zero.ToolChoice.Mode != ToolChoiceAuto || zero.Reasoning.Mode != ReasoningDefault {
		t.Fatalf("zero Settings selects a control instead of leaving all controls unset: %#v", zero)
	}

	zeroFloat := 0.0
	zeroInt := 0
	set := Settings{Temperature: &zeroFloat, TopP: &zeroFloat, MaxOutputTokens: &zeroInt}
	snapshot := cloneSettings(set)
	if settingsAreZero(set) || snapshot.Temperature == nil || snapshot.TopP == nil || snapshot.MaxOutputTokens == nil {
		t.Fatalf("deliberate pointer zero was treated as unset: original=%#v copy=%#v", set, snapshot)
	}
	if *snapshot.Temperature != 0 || *snapshot.TopP != 0 || *snapshot.MaxOutputTokens != 0 {
		t.Fatalf("deliberate zero values changed in snapshot: %#v", snapshot)
	}
	if snapshot.Temperature == set.Temperature || snapshot.TopP == set.TopP || snapshot.MaxOutputTokens == set.MaxOutputTokens {
		t.Fatal("Settings snapshot retained caller-owned pointer storage")
	}
}

func TestReasoningConfigExpressesEveryNeutralShape(t *testing.T) {
	// R-3QUG-OHV9
	configs := []ReasoningConfig{
		{Mode: ReasoningDefault},
		{Mode: ReasoningOff},
		{Mode: ReasoningOn},
		{Mode: ReasoningEffort, Effort: EffortLow},
		{Mode: ReasoningEffort, Effort: EffortMedium},
		{Mode: ReasoningEffort, Effort: EffortHigh},
		{Mode: ReasoningBudget, Budget: 8000},
	}
	capabilities := wireCapabilities{
		name:      "test grammar",
		reasoning: reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
	}
	seen := make(map[ReasoningMode]bool)
	for _, config := range configs {
		if err := capabilities.validate(Settings{Reasoning: config}); err != nil {
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
}

func TestAmbiguousNeutralSettingsAreInvalid(t *testing.T) {
	capabilities := wireCapabilities{
		name:       "complete grammar",
		reasoning:  reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
		toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
	}
	tests := []Settings{
		{Reasoning: ReasoningConfig{Mode: ReasoningDefault, Effort: EffortLow}},
		{Reasoning: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh, Budget: 1}},
		{Reasoning: ReasoningConfig{Mode: ReasoningBudget, Budget: 0}},
		{ToolChoice: ToolChoice{Mode: ToolChoiceAuto, Name: "lookup"}},
		{ToolChoice: ToolChoice{Mode: ToolChoiceTool}},
	}
	for _, settings := range tests {
		if err := capabilities.validate(settings); err == nil {
			t.Errorf("ambiguous settings accepted: %#v", settings)
		}
	}
}
