// Package model resolves the user-supplied --model value into a
// (provider, bare-API-ID) pair.
//
// R-XBYO-1ZI1: --model accepts the bare API model ID; per-provider
// short aliases are accepted as sugar (Anthropic: opus/sonnet/haiku,
// each with an optional [1m] suffix). The provider is inferred from
// the bare ID's prefix; unknown prefixes are a fatal startup error.
//
// R-Y23Q-MNSU pins the prefix → provider mapping; this package is
// the single place that mapping lives. Registry membership and
// effort validation (R-ZCFX-5XZ8 / R-ZX67-O1L1) are out of scope
// here — this layer's job is parse and infer, not vet.
package model

import (
	"errors"
	"fmt"
	"strings"
)

type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
)

type Resolved struct {
	Provider Provider
	BareID   string
}

// anthropicAliases maps the documented Anthropic short aliases to
// their bare API IDs. Per R-XBYO-1ZI1 each may carry a [1m] suffix
// that is preserved through resolution.
var anthropicAliases = map[string]string{
	"opus":   "claude-opus-4-7",
	"sonnet": "claude-sonnet-4-6",
	"haiku":  "claude-haiku-4-5",
}

// googleAliases maps the documented Google short aliases to their
// bare API IDs. R-XBYO-1ZI1, R-Y23Q-MNSU.
var googleAliases = map[string]string{
	"pro": "gemini-3.1-pro-preview",
}

// Resolve turns the raw --model value (a bare API ID or an alias)
// into a Resolved pair. An empty value or unknown-prefix bare ID
// returns a fatal startup error.
func Resolve(input string) (Resolved, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Resolved{}, errors.New("--model is required")
	}

	bare := resolveAlias(trimmed)

	switch {
	case strings.HasPrefix(bare, "claude-"):
		return Resolved{Provider: ProviderAnthropic, BareID: bare}, nil
	case strings.HasPrefix(bare, "gpt-"):
		return Resolved{Provider: ProviderOpenAI, BareID: bare}, nil
	case strings.HasPrefix(bare, "gemini-"):
		return Resolved{Provider: ProviderGoogle, BareID: bare}, nil
	default:
		return Resolved{}, fmt.Errorf("--model %q: unknown provider prefix (expected claude-*, gpt-*, or gemini-*)", input)
	}
}

// resolveAlias maps provider short aliases to their bare API IDs.
// Anthropic aliases may carry a [1m] suffix that is preserved.
// Inputs that don't match any alias pass through unchanged.
func resolveAlias(in string) string {
	stem, suffix := splitBracket(in)
	if bare, ok := anthropicAliases[stem]; ok {
		return bare + suffix
	}
	if bare, ok := googleAliases[stem]; ok {
		return bare + suffix
	}
	return in
}

// splitBracket separates a trailing [...] suffix from the stem.
// "haiku[1m]" -> ("haiku", "[1m]"); "claude-haiku-4-5" -> (whole, "").
func splitBracket(in string) (string, string) {
	if !strings.HasSuffix(in, "]") {
		return in, ""
	}
	open := strings.LastIndex(in, "[")
	if open <= 0 {
		return in, ""
	}
	return in[:open], in[open:]
}
