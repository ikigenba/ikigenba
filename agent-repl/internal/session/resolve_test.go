package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

// R-RIVL-2RE0
func TestConfigContract(t *testing.T) {
	provider, model, wire, auth := "provider", "model", "wire", "auth"
	authFile, baseURL, systemFile := "auth-file", "base-url", "system-file"
	settings := map[string]string{"key": "value"}
	home, root := "home", "root"
	getenv := func(string) string { return "value" }
	log := &agentkit.Log{}
	cfg := Config{
		Provider:   provider,
		Model:      model,
		Wire:       wire,
		Auth:       auth,
		AuthFile:   authFile,
		BaseURL:    baseURL,
		SystemFile: systemFile,
		Settings:   settings,
		Home:       home,
		Getenv:     getenv,
		Root:       root,
		Log:        log,
	}
	if cfg.Provider != provider || cfg.Model != model || cfg.Wire != wire || cfg.Auth != auth ||
		cfg.AuthFile != authFile || cfg.BaseURL != baseURL || cfg.SystemFile != systemFile ||
		!reflect.DeepEqual(cfg.Settings, settings) || cfg.Home != home || cfg.Getenv("KEY") != "value" ||
		cfg.Root != root || cfg.Log != log {
		t.Fatalf("Config = %+v, want the constructed field values", cfg)
	}
}

// R-RK3H-GJ4P
func TestPlanContract(t *testing.T) {
	var offering agentkit.Offering
	var mode agentkit.AuthMode
	model, envVar, authFile, baseURL := "model", "ENV", "auth-file", "base-url"
	plan := Plan{Offering: offering, Model: model, AuthMode: mode, EnvVar: envVar, AuthFile: authFile, BaseURL: baseURL}
	if !reflect.DeepEqual(plan.Offering, offering) || plan.AuthMode != mode || plan.Model != model ||
		plan.EnvVar != envVar || plan.AuthFile != authFile || plan.BaseURL != baseURL {
		t.Fatalf("Plan = %+v, want the constructed field values", plan)
	}
}

// R-ANGT-BS97
func TestSessionAPIContract(_ *testing.T) {
	declared := func(
		func(Config) (Plan, error),
		func(Config) (*Session, error),
		func(*Session) Plan,
		func(*Session, context.Context, string) *agentkit.Stream,
		func(*Session) error,
	) {
	}
	declared(Resolve, Open, (*Session).Plan, (*Session).Send, (*Session).Close)
}

// R-UY6Y-9ABY
func TestResolveCatalogedOffering(t *testing.T) {
	cfg := resolveConfig(t)
	cfg.Model = "gpt-5.6-sol"
	cfg.Wire = "responses"
	got, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := openAIResponsesOffering("gpt-5.6-sol", 1_050_000, agentkit.Pricing{Tiers: []agentkit.RateTier{
		{MinInputTokens: 0, InputUncached: 5000, CacheReadInput: 500, Output: 30000},
	}})
	assertOffering(t, got.Offering, want)
	if got.Model != "gpt-5.6-sol" {
		t.Fatalf("cataloged model = %q, want %q", got.Model, "gpt-5.6-sol")
	}
}

// R-UZEU-N22N
func TestResolveOffCatalogOfferingWithExplicitWire(t *testing.T) {
	cfg := resolveConfig(t)
	cfg.Model = "model-released-after-catalog"
	cfg.Wire = "chat"
	got, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Offering.Host != agentkit.HostOpenAI || got.Offering.WireName != agentkit.WireChat ||
		got.Offering.WireModel != cfg.Model || got.Model != cfg.Model {
		t.Fatalf("off-catalog plan = %+v, want OpenAI chat transport with model %q", got, cfg.Model)
	}
	if got.Offering.ID != agentkit.OfferingOpenAIChat {
		t.Fatalf("offering id = %q, want first matching %q", got.Offering.ID, agentkit.OfferingOpenAIChat)
	}
}

// R-UZEU-N22N
func TestResolveOffCatalogOfferingWithoutWireUsesHostsFirstOffering(t *testing.T) {
	cfg := resolveConfig(t)
	cfg.Model = "model-released-after-catalog"
	cfg.Wire = ""
	got, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := openAIResponsesOffering(cfg.Model, 1_050_000, agentkit.Pricing{Tiers: []agentkit.RateTier{
		{MinInputTokens: 0, InputUncached: 2500, CacheReadInput: 250, Output: 15000},
		{MinInputTokens: 272_001, InputUncached: 5000, CacheReadInput: 500, Output: 22500},
	}})
	want.Endpoints = want.Endpoints[:1]
	want.Reasoning.Default.Effort = agentkit.EffortNone
	assertOffering(t, got.Offering, want)
	if got.Model != cfg.Model {
		t.Fatalf("off-catalog model = %q, want %q", got.Model, cfg.Model)
	}
}

func openAIResponsesOffering(wireModel string, contextWindow int64, pricing agentkit.Pricing) agentkit.Offering {
	return agentkit.Offering{
		ID:       agentkit.OfferingOpenAIResponses,
		Host:     agentkit.HostOpenAI,
		WireName: agentkit.WireResponses,
		Endpoints: []agentkit.EndpointSpec{
			{AuthMode: agentkit.AuthModeAPIKey, BaseURL: "https://api.openai.com/v1/responses"},
			{
				AuthMode: agentkit.AuthModeOAuth,
				BaseURL:  "https://chatgpt.com/backend-api/codex/responses",
				Rotation: agentkit.Rotation{
					RefreshURL: "https://auth.openai.com/oauth/token",
					ClientID:   strings.Join([]string{"app", "EMoamEEZ73f0CkXaXp7hrann"}, "_"),
				},
			},
		},
		WireModel: wireModel,
		Context:   contextWindow,
		Pricing:   pricing,
		Reasoning: agentkit.ReasoningSpec{
			Kind:       agentkit.ReasoningKindEffort,
			Term:       "effort",
			Levels:     []agentkit.Effort{agentkit.EffortNone, agentkit.EffortLow, agentkit.EffortMedium, agentkit.EffortHigh, agentkit.EffortXHigh},
			CanDisable: true,
			Default:    agentkit.ReasoningConfig{Mode: agentkit.ReasoningEffort, Effort: agentkit.EffortMedium},
		},
	}
}

func assertOffering(t *testing.T, got, want agentkit.Offering) {
	t.Helper()
	if wireType := fmt.Sprintf("%T", got.WireFormat); wireType != "*agentkit.openAIResponsesWire" {
		t.Errorf("offering wire format type = %q, want %q", wireType, "*agentkit.openAIResponsesWire")
	}
	got.WireFormat = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("offering = %+v, want %+v", got, want)
	}
}

// R-V1UN-ELK1
func TestResolveEnvironmentVariableAndAuthPaths(t *testing.T) {
	cfg := resolveConfig(t)
	cfg.Home = "/phase-6-test-home"
	got, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := "/phase-6-test-home/.agent-repl/openai-auth.json"
	if got.EnvVar != "OPENAI_API_KEY" || got.AuthFile != wantDefault {
		t.Fatalf("environment/auth file = (%q, %q), want (%q, %q)", got.EnvVar, got.AuthFile, "OPENAI_API_KEY", wantDefault)
	}
	cfg.AuthFile = filepath.Join(t.TempDir(), "chosen.json")
	got, err = Resolve(cfg)
	if err != nil || got.AuthFile != cfg.AuthFile {
		t.Fatalf("override auth file = %q, error %v, want %q", got.AuthFile, err, cfg.AuthFile)
	}
}

// authWord matches "auth" as a whole word, so "oauth" alone cannot satisfy it.
var authWord = regexp.MustCompile(`\bauth\b`)

// R-B4VH-WD0F
func TestResolveAuthenticationMode(t *testing.T) {
	cfg := resolveConfig(t)
	plan, err := Resolve(cfg)
	if err != nil || plan.AuthMode != agentkit.AuthModeAPIKey {
		t.Fatalf("missing token mode = %q, error %v, want api_key", plan.AuthMode, err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.AuthFile), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.AuthFile, []byte(`{"access_token":"token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = Resolve(cfg)
	if err != nil || plan.AuthMode != agentkit.AuthModeOAuth {
		t.Fatalf("existing token mode = %q, error %v, want oauth", plan.AuthMode, err)
	}
	cfg.Provider = "anthropic"
	cfg.Model = "claude-sonnet-5"
	cfg.AuthFile = plan.AuthFile
	plan, err = Resolve(cfg)
	if err != nil || plan.AuthMode != agentkit.AuthModeAPIKey {
		t.Fatalf("existing token with non-OAuth offering mode = %q, error %v, want api_key", plan.AuthMode, err)
	}
	cfg.Auth = "api_key"
	plan, err = Resolve(cfg)
	if err != nil || plan.AuthMode != agentkit.AuthModeAPIKey {
		t.Fatalf("explicit mode = %q, error %v, want api_key", plan.AuthMode, err)
	}
	cfg.Auth = "oauth"
	_, err = Resolve(cfg)
	if err == nil || !authWord.MatchString(err.Error()) {
		t.Fatalf("auth absent from offering error = %v, want diagnostic naming auth as a whole word", err)
	}
}

// R-B63E-A4R4
func TestResolveBaseURL(t *testing.T) {
	cfg := resolveConfig(t)
	plan, err := Resolve(cfg)
	const platformBaseURL = "https://api.openai.com/v1/responses"
	if err != nil || plan.BaseURL != platformBaseURL {
		t.Fatalf("API-key base URL = %q, error %v, want %q", plan.BaseURL, err, platformBaseURL)
	}
	cfg.Auth = "oauth"
	plan, err = Resolve(cfg)
	const oauthBaseURL = "https://chatgpt.com/backend-api/codex/responses"
	if err != nil || plan.BaseURL != oauthBaseURL {
		t.Fatalf("OAuth base URL = %q, error %v, want %q", plan.BaseURL, err, oauthBaseURL)
	}
	cfg.BaseURL = "https://loopback.example/v1"
	plan, err = Resolve(cfg)
	if err != nil || plan.BaseURL != cfg.BaseURL {
		t.Fatalf("override base URL = %q, error %v, want %q", plan.BaseURL, err, cfg.BaseURL)
	}
}

func resolveConfig(t *testing.T) Config {
	t.Helper()
	return Config{Provider: "openai", Model: "gpt-5.6-sol", Home: t.TempDir()}
}
