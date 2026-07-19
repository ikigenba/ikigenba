package runner

import (
	"testing"

	"prompts/internal/prompt"
)

func TestGenSettings_MaxTokensOmittedByDefault(t *testing.T) {
	cfg := prompt.Config{Provider: "anthropic", Model: "claude-haiku-4-5"}
	gen := genSettings(cfg)

	if gen.MaxTokens != 0 {
		t.Errorf("MaxTokens = %d, want zero-value provider default", gen.MaxTokens)
	}
}

func TestGenSettings_MaxTokensHonorsConfig(t *testing.T) {
	cfg := prompt.Config{Provider: "anthropic", Model: "claude-haiku-4-5", MaxTokens: 12345}
	gen := genSettings(cfg)

	if gen.MaxTokens != 12345 {
		t.Errorf("MaxTokens = %d, want explicit 12345", gen.MaxTokens)
	}
}

// Reasoning precedence: effort → thinking_budget → thinking_level →
// thinking, first field that is set wins.
func TestGenSettings_ReasoningPrecedence(t *testing.T) {
	budget := 4096
	thinkingOff := false

	// effort wins over every lower-precedence field.
	cfg := prompt.Config{
		Effort:         "high",
		ThinkingLevel:  "medium",
		ThinkingBudget: &budget,
		Thinking:       &thinkingOff,
	}
	if level, ok := genSettings(cfg).Reasoning.Level(); !ok || level != "high" {
		t.Fatalf("effort precedence: Reasoning level = %q (ok=%v), want \"high\"", level, ok)
	}

	// thinking_budget wins when effort is empty.
	cfg = prompt.Config{ThinkingLevel: "medium", ThinkingBudget: &budget, Thinking: &thinkingOff}
	if got, ok := genSettings(cfg).Reasoning.Budget(); !ok || got != budget {
		t.Fatalf("thinking_budget precedence: Reasoning budget = %d (ok=%v), want %d", got, ok, budget)
	}

	// thinking_level wins when effort and thinking_budget are empty.
	cfg = prompt.Config{ThinkingLevel: "medium", Thinking: &thinkingOff}
	if level, ok := genSettings(cfg).Reasoning.Level(); !ok || level != "medium" {
		t.Fatalf("thinking_level precedence: Reasoning level = %q (ok=%v), want \"medium\"", level, ok)
	}

	// thinking (disabled) applies only when all higher-precedence fields are unset.
	cfg = prompt.Config{Thinking: &thinkingOff}
	if !genSettings(cfg).Reasoning.Disabled() {
		t.Fatalf("thinking precedence: Reasoning not disabled, want disabled")
	}
}
