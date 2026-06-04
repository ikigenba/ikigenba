package model

import (
	"strings"
	"testing"
)

// R-XBYO-1ZI1: --model accepts the bare API model ID; aliases
// resolve before inference; unknown prefixes are a fatal startup
// error.
func TestR_XBYO_1ZI1_ModelFlagResolvesProviderFromPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantProv Provider
		wantBare string
	}{
		{"bare anthropic", "claude-haiku-4-5", ProviderAnthropic, "claude-haiku-4-5"},
		{"bare openai", "gpt-5.4", ProviderOpenAI, "gpt-5.4"},
		{"bare google", "gemini-3-pro-preview", ProviderGoogle, "gemini-3-pro-preview"},
		{"alias haiku", "haiku", ProviderAnthropic, "claude-haiku-4-5"},
		{"alias opus", "opus", ProviderAnthropic, "claude-opus-4-7"},
		{"alias sonnet", "sonnet", ProviderAnthropic, "claude-sonnet-4-6"},
		{"alias bracketed", "opus[1m]", ProviderAnthropic, "claude-opus-4-7[1m]"},
		{"bare anthropic bracketed", "claude-sonnet-4-6[1m]", ProviderAnthropic, "claude-sonnet-4-6[1m]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.input)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tc.input, err)
			}
			if got.Provider != tc.wantProv {
				t.Errorf("provider = %q, want %q", got.Provider, tc.wantProv)
			}
			if got.BareID != tc.wantBare {
				t.Errorf("bare = %q, want %q", got.BareID, tc.wantBare)
			}
		})
	}
}

func TestR_XBYO_1ZI1_UnknownPrefixIsFatal(t *testing.T) {
	cases := []string{"llama-3", "mistral-large", "bogus", "claud-typo"}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := Resolve(in)
			if err == nil {
				t.Fatalf("Resolve(%q) returned nil error, want fatal startup error", in)
			}
			if !strings.Contains(err.Error(), "unknown provider prefix") {
				t.Errorf("error = %q, want it to mention unknown provider prefix", err.Error())
			}
		})
	}
}

func TestR_XBYO_1ZI1_EmptyModelIsError(t *testing.T) {
	for _, in := range []string{"", "   ", "\t"} {
		if _, err := Resolve(in); err == nil {
			t.Errorf("Resolve(%q) returned nil error, want required-flag error", in)
		}
	}
}
