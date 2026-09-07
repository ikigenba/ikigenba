package model_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
)

func TestResolveCatalogedOffering(t *testing.T) {
	// R-I5VD-FM7R
	cfg, fixed := fixedCatalogOffering(t)
	want, err := agentkit.Lookup(cfg.Model, agentkit.Host(cfg.Provider), agentkit.WireName(cfg.Wire))
	if err != nil {
		t.Fatalf("Lookup fixture: %v", err)
	}
	assertOffering(t, "configured-wire Lookup", want, fixed)
	got := mustResolve(t, cfg)
	assertOffering(t, "configured-wire Resolve", got.Offering, fixed)
	if got.Model != "claude-fable-5" {
		t.Errorf("Model = %q, want wire model claude-fable-5", got.Model)
	}

	cfg.Wire = ""
	want, err = agentkit.Lookup(cfg.Model, agentkit.Host(cfg.Provider), "")
	if err != nil {
		t.Fatalf("default-wire Lookup fixture: %v", err)
	}
	assertOffering(t, "default-wire Lookup", want, fixed)
	got = mustResolve(t, cfg)
	assertOffering(t, "default-wire Resolve", got.Offering, fixed)
	if got.Model != "claude-fable-5" {
		t.Errorf("default-wire model = %q, want claude-fable-5", got.Model)
	}

	cfg.Provider = "provider-guaranteed-absent"
	if _, err := model.Resolve(cfg); err == nil {
		t.Error("cataloged model with an invalid provider resolved as an off-catalog model")
	}
}

func fixedCatalogOffering(t *testing.T) (model.Config, agentkit.Offering) {
	t.Helper()
	return model.Config{
			Provider:   "anthropic",
			Model:      "claude-fable-5",
			Wire:       "messages",
			MaxContext: -1,
			Home:       t.TempDir(),
		}, agentkit.Offering{
			ID:         agentkit.OfferingAnthropicMessages,
			Host:       agentkit.HostAnthropic,
			WireName:   agentkit.WireMessages,
			WireFormat: agentkit.AnthropicMessagesWire(),
			Endpoints: []agentkit.EndpointSpec{{
				AuthMode: agentkit.AuthModeAPIKey,
				BaseURL:  "https://api.anthropic.com/v1/messages",
			}},
			WireModel:       "claude-fable-5",
			Context:         1_000_000,
			MaxOutputTokens: 128_000,
			Pricing: agentkit.Pricing{Tiers: []agentkit.RateTier{{
				InputUncached:  10_000,
				CacheReadInput: 1_000,
				CacheWrite5m:   12_500,
				CacheWrite1h:   20_000,
				Output:         50_000,
			}}},
			Reasoning: agentkit.ReasoningSpec{
				Kind:    agentkit.ReasoningKindEffort,
				Term:    "effort",
				Levels:  []agentkit.Effort{agentkit.EffortLow, agentkit.EffortMedium, agentkit.EffortHigh, agentkit.EffortXHigh, agentkit.EffortMax},
				Default: agentkit.ReasoningConfig{Mode: agentkit.ReasoningEffort, Effort: agentkit.EffortMedium},
			},
		}
}

func TestResolveOffCatalogOffering(t *testing.T) {
	// R-I5VD-FM7R
	modelName := absentModel(t)
	for _, explicitWire := range []bool{false, true} {
		_, fixed := fixedCatalogOffering(t)
		cfg := model.Config{
			Provider:   "anthropic",
			Model:      modelName,
			MaxContext: -1,
			Home:       t.TempDir(),
		}
		if explicitWire {
			cfg.Wire = "messages"
		}
		borrowed := firstOffering(t, cfg.Provider, cfg.Wire)
		assertOffering(t, "first matching catalog offering", borrowed, fixed)
		got := mustResolve(t, cfg)
		fixed.WireModel = modelName
		fixed.Context = 0
		assertOffering(t, "off-catalog Resolve", got.Offering, fixed)
		if got.Model != modelName {
			t.Errorf("explicitWire=%v: Model = %q, want %q", explicitWire, got.Model, modelName)
		}
	}

	_, err := model.Resolve(model.Config{Provider: "provider-guaranteed-absent", Model: modelName, MaxContext: -1, Home: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "provider-guaranteed-absent") {
		t.Fatalf("missing transport error = %v, want useful provider error", err)
	}
}

func TestResolveCredentialNames(t *testing.T) {
	// R-I739-TDYG
	cfg := catalogConfig(t)
	cfg.Provider = strings.ToLower(cfg.Provider)
	cfg.AuthFile = filepath.Join(t.TempDir(), "chosen-token.json")
	got := mustResolve(t, cfg)
	if got.EnvVar != strings.ToUpper(cfg.Provider)+"_API_KEY" {
		t.Errorf("EnvVar = %q", got.EnvVar)
	}
	if got.AuthFile != cfg.AuthFile {
		t.Errorf("AuthFile = %q, want explicit %q", got.AuthFile, cfg.AuthFile)
	}

	cfg.AuthFile = ""
	got = mustResolve(t, cfg)
	want := filepath.Join(cfg.Home, ".dory", cfg.Provider+"-auth.json")
	if got.AuthFile != want {
		t.Errorf("default AuthFile = %q, want %q", got.AuthFile, want)
	}
}

func TestResolveDefaultAndExplicitAuth(t *testing.T) {
	// R-I8B6-75P5
	cfg, offering := oauthConfig(t)
	got := mustResolve(t, cfg)
	// The expected mode is determined by the absent fixture file, independently
	// of the mode returned by Resolve.
	if got.AuthMode != agentkit.AuthModeAPIKey {
		t.Fatalf("auth without token file = %q, want api_key", got.AuthMode)
	}
	if err := os.MkdirAll(filepath.Dir(got.AuthFile), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(got.AuthFile, []byte("not parsed in resolution"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got = mustResolve(t, cfg)
	// The only changed input is existence of the exact token path.
	if got.AuthMode != agentkit.AuthModeOAuth {
		t.Errorf("auth with existing token file = %q, want oauth", got.AuthMode)
	}

	cfg.Auth = string(agentkit.AuthModeAPIKey)
	got = mustResolve(t, cfg)
	if got.AuthMode != agentkit.AuthModeAPIKey {
		t.Errorf("explicit auth = %q, want api_key", got.AuthMode)
	}

	unavailable := agentkit.AuthMode("unavailable")
	for _, endpoint := range offering.Endpoints {
		if endpoint.AuthMode == unavailable {
			t.Fatal("test auth mode unexpectedly available")
		}
	}
	cfg.Auth = string(unavailable)
	_, err := model.Resolve(cfg)
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("unsupported auth error = %v, want error naming auth", err)
	}
}

func TestResolveBaseURL(t *testing.T) {
	// R-I9J2-KXFU
	cfg := catalogConfig(t)
	got := mustResolve(t, cfg)
	want := endpoint(t, got.Offering, got.AuthMode).BaseURL
	if got.BaseURL != want {
		t.Errorf("BaseURL = %q, want endpoint URL %q", got.BaseURL, want)
	}

	cfg.BaseURL = "https://example.invalid/custom-prefix"
	got = mustResolve(t, cfg)
	if got.BaseURL != cfg.BaseURL {
		t.Errorf("BaseURL = %q, want override %q", got.BaseURL, cfg.BaseURL)
	}
}

func TestResolveMaxContext(t *testing.T) {
	// R-IAQY-YP6J
	cfg := catalogConfig(t)
	for _, value := range []int64{0, 12345} {
		cfg.MaxContext = value
		if got := mustResolve(t, cfg).MaxContext; got != value {
			t.Errorf("MaxContext with override %d = %d", value, got)
		}
	}
	cfg.MaxContext = -1
	got := mustResolve(t, cfg)
	if got.MaxContext != got.Offering.Context {
		t.Errorf("default MaxContext = %d, want offering context %d", got.MaxContext, got.Offering.Context)
	}

	borrowed := firstOffering(t, "", "")
	offCatalog := model.Config{Provider: string(borrowed.Host), Model: absentModel(t), MaxContext: -1, Home: t.TempDir()}
	got = mustResolve(t, offCatalog)
	if got.MaxContext != 0 || got.Offering.Context != 0 {
		t.Errorf("off-catalog context = plan %d/offering %d, want zero", got.MaxContext, got.Offering.Context)
	}
}

func catalogConfig(t *testing.T) model.Config {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if _, ok := findEndpoint(offering, agentkit.AuthModeAPIKey); ok {
				return model.Config{
					Provider:   string(offering.Host),
					Model:      entry.Model,
					Wire:       string(offering.WireName),
					MaxContext: -1,
					Home:       t.TempDir(),
				}
			}
		}
	}
	t.Fatal("catalog has no API-key offering")
	return model.Config{}
}

func oauthConfig(t *testing.T) (model.Config, agentkit.Offering) {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			_, hasOAuth := findEndpoint(offering, agentkit.AuthModeOAuth)
			_, hasAPIKey := findEndpoint(offering, agentkit.AuthModeAPIKey)
			if hasOAuth && hasAPIKey {
				return model.Config{
					Provider:   string(offering.Host),
					Model:      entry.Model,
					Wire:       string(offering.WireName),
					MaxContext: -1,
					Home:       t.TempDir(),
				}, offering
			}
		}
	}
	t.Fatal("catalog has no offering with OAuth and API-key endpoints")
	return model.Config{}, agentkit.Offering{}
}

func absentModel(t *testing.T) string {
	t.Helper()
	const candidate = "dory-off-catalog-model-guaranteed-absent"
	for _, entry := range agentkit.Catalog() {
		if entry.Model == candidate {
			t.Fatalf("fixture model %q unexpectedly entered the catalog", candidate)
		}
	}
	return candidate
}

func firstOffering(t *testing.T, provider, wire string) agentkit.Offering {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if provider != "" && offering.Host != agentkit.Host(provider) {
				continue
			}
			if wire != "" && offering.WireName != agentkit.WireName(wire) {
				continue
			}
			return offering
		}
	}
	t.Fatalf("catalog has no offering for provider %q wire %q", provider, wire)
	return agentkit.Offering{}
}

func mustResolve(t *testing.T, cfg model.Config) model.Plan {
	t.Helper()
	got, err := model.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve(%#v): %v", cfg, err)
	}
	return got
}

func endpoint(t *testing.T, offering agentkit.Offering, mode agentkit.AuthMode) agentkit.EndpointSpec {
	t.Helper()
	got, ok := findEndpoint(offering, mode)
	if !ok {
		t.Fatalf("offering has no endpoint for auth %q", mode)
	}
	return got
}

func findEndpoint(offering agentkit.Offering, mode agentkit.AuthMode) (agentkit.EndpointSpec, bool) {
	for _, candidate := range offering.Endpoints {
		if candidate.AuthMode == mode {
			return candidate, true
		}
	}
	return agentkit.EndpointSpec{}, false
}

func assertOffering(t *testing.T, label string, got, want agentkit.Offering) {
	t.Helper()
	if reflect.TypeOf(got.WireFormat) != reflect.TypeOf(want.WireFormat) {
		t.Fatalf("%s wire type = %T, want %T", label, got.WireFormat, want.WireFormat)
	}
	// WireFormat is an opaque codec containing function values. Its concrete
	// type is checked above; align its instance to compare every other field.
	want.WireFormat = got.WireFormat
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s offering = %#v, want fixed fixture %#v", label, got, want)
	}
}
