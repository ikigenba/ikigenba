package provider

import (
	"strings"
	"testing"

	"prompts/internal/prompt"
)

func TestBuildConstructsEveryConfiguredProvider(t *testing.T) {
	// R-K5I9-YGS9
	tests := []struct {
		provider string
		envKey   string
		name     string
	}{
		{provider: "anthropic", envKey: "ANTHROPIC_API_KEY", name: "anthropic"},
		{provider: "openai", envKey: "OPENAI_API_KEY", name: "openai.apikey"},
		{provider: "google", envKey: "GEMINI_API_KEY", name: "google"},
		{provider: "zai", envKey: "ZAI_API_KEY", name: "zai"},
		{provider: "openrouter", envKey: "OPENROUTER_API_KEY", name: "openrouter"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			var requested string
			built, err := Build(prompt.Config{
				Provider: tt.provider,
				BaseURL:  "https://provider.example.test/v1",
			}, func(key string) string {
				requested = key
				return " test-key "
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if requested != tt.envKey {
				t.Fatalf("requested env key = %q, want %q", requested, tt.envKey)
			}
			if got := built.Name(); got != tt.name {
				t.Fatalf("provider name = %q, want %q", got, tt.name)
			}
		})
	}
}

func TestBuildRejectsMissingKeyAndUnknownProvider(t *testing.T) {
	if _, err := Build(prompt.Config{Provider: "openai"}, func(string) string { return "  " }); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing-key error = %v", err)
	}
	if _, err := Build(prompt.Config{Provider: "unknown"}, func(string) string { return "key" }); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unknown-provider error = %v", err)
	}
}

func TestBuildEmbedderSupportsOpenAIAndGoogleOnly(t *testing.T) {
	for _, providerName := range []string{"openai", "google"} {
		embedder, err := BuildEmbedder(providerName, func(string) string { return "key" })
		if err != nil {
			t.Fatalf("BuildEmbedder(%q): %v", providerName, err)
		}
		if embedder == nil || embedder.Name() == "" {
			t.Fatalf("BuildEmbedder(%q) returned invalid embedder", providerName)
		}
	}
	if _, err := BuildEmbedder("anthropic", func(string) string { return "key" }); err == nil || !strings.Contains(err.Error(), "does not support embeddings") {
		t.Fatalf("unsupported embedder error = %v", err)
	}
}
