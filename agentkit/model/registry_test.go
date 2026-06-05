package model

import (
	"strings"
	"testing"
)

// R-YRPM-NUDF: registry is a const-style data table; MVP entries for
// the Anthropic backend (R-MPR7-P0A4) must be present and accepted by
// Validate without per-call configuration.
func TestR_YRPM_NUDF_RegistryHoldsHaikuMVPEntry(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-haiku-4-5"}
	if err := Validate(r); err != nil {
		t.Fatalf("Validate(haiku MVP) = %v, want nil", err)
	}

	models, ok := registry[ProviderAnthropic]
	if !ok {
		t.Fatalf("registry missing Anthropic provider row")
	}
	if _, ok := models["claude-haiku-4-5"]; !ok {
		t.Errorf("Anthropic registry missing claude-haiku-4-5; have %v", supportedModels(models))
	}
}

// R-ZCFX-5XZ8: a --model that parses to a known provider but is not
// in the registry is rejected at startup; the error must list the
// supported models for that provider so the user can recover.
func TestR_ZCFX_5XZ8_UnknownModelRejectedWithSupportedList(t *testing.T) {
	cases := []struct {
		name string
		in   Resolved
	}{
		{"opus not in MVP", Resolved{ProviderAnthropic, "claude-opus-4-7"}},
		{"haiku 1m variant not in MVP", Resolved{ProviderAnthropic, "claude-haiku-4-5[1m]"}},
		{"openai deferred", Resolved{ProviderOpenAI, "gpt-5.4"}},
		{"google deferred", Resolved{ProviderGoogle, "gemini-3-pro-preview"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.in)
			if err == nil {
				t.Fatalf("Validate(%+v) returned nil, want rejection", tc.in)
			}
			if tc.in.Provider == ProviderAnthropic {
				if !strings.Contains(err.Error(), "claude-haiku-4-5") {
					t.Errorf("Anthropic rejection %q must list claude-haiku-4-5", err.Error())
				}
				if !strings.Contains(err.Error(), "claude-sonnet-4-6") {
					t.Errorf("Anthropic rejection %q must list claude-sonnet-4-6", err.Error())
				}
			}
		})
	}
}

// R-MPR7-P0A4: Haiku 4.5 takes no --effort argument; supplying one
// must be rejected with an error naming the supported value as
// "(none)". An empty effort is the MVP default and must be accepted.
func TestR_MPR7_P0A4_HaikuRejectsEffort(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-haiku-4-5"}
	for _, effort := range []string{"low", "medium", "high", "minimal", "thinking"} {
		t.Run(effort, func(t *testing.T) {
			err := ValidateEffort(r, effort)
			if err == nil {
				t.Fatalf("ValidateEffort(haiku, %q) = nil, want rejection", effort)
			}
			if !strings.Contains(err.Error(), "(none)") {
				t.Errorf("error %q must name supported value as (none)", err.Error())
			}
		})
	}
}

func TestR_MPR7_P0A4_HaikuAcceptsNoEffort(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-haiku-4-5"}
	if err := ValidateEffort(r, ""); err != nil {
		t.Fatalf("ValidateEffort(haiku, \"\") = %v, want nil", err)
	}
}

// R-MPR7-P0A4: Sonnet 4.6 accepts effort values low|medium|high|xhigh|max
// and rejects any other non-empty value.
func TestR_MPR7_P0A4_SonnetAcceptsValidEfforts(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-sonnet-4-6"}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		t.Run(effort, func(t *testing.T) {
			if err := ValidateEffort(r, effort); err != nil {
				t.Errorf("ValidateEffort(sonnet, %q) = %v, want nil", effort, err)
			}
		})
	}
}

func TestR_MPR7_P0A4_SonnetAcceptsNoEffort(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-sonnet-4-6"}
	if err := ValidateEffort(r, ""); err != nil {
		t.Fatalf("ValidateEffort(sonnet, \"\") = %v, want nil", err)
	}
}

func TestR_MPR7_P0A4_SonnetRejectsUnknownEffort(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-sonnet-4-6"}
	for _, effort := range []string{"minimal", "thinking", "turbo", "fast"} {
		t.Run(effort, func(t *testing.T) {
			err := ValidateEffort(r, effort)
			if err == nil {
				t.Fatalf("ValidateEffort(sonnet, %q) = nil, want rejection", effort)
			}
			if !strings.Contains(err.Error(), "supported values:") {
				t.Errorf("error %q must list values via \"supported values:\"", err.Error())
			}
		})
	}
}

// R-MPR7-P0A4: both MVP models must be present in the registry so
// Validate accepts them without per-call configuration.
func TestR_MPR7_P0A4_BothModelsInRegistry(t *testing.T) {
	models, ok := registry[ProviderAnthropic]
	if !ok {
		t.Fatal("registry missing Anthropic provider row")
	}
	for _, id := range []string{"claude-haiku-4-5", "claude-sonnet-4-6"} {
		if _, ok := models[id]; !ok {
			t.Errorf("Anthropic registry missing %q; have %v", id, supportedModels(models))
		}
		r := Resolved{Provider: ProviderAnthropic, BareID: id}
		if err := Validate(r); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", id, err)
		}
	}
}

// R-ZX67-O1L1: a --effort value that is not legal for the selected
// model is rejected at startup with an error listing the legal values
// for that model. The legal-values listing is the user-facing recovery
// signal; this test pins the "supported values:" reporting contract
// that all models with a non-empty effort vocabulary must satisfy.
func TestR_ZX67_O1L1_IllegalEffortListsSupportedValues(t *testing.T) {
	r := Resolved{Provider: ProviderAnthropic, BareID: "claude-haiku-4-5"}
	err := ValidateEffort(r, "high")
	if err == nil {
		t.Fatal("expected error for illegal effort")
	}
	msg := err.Error()
	if !strings.Contains(msg, "supported values:") {
		t.Errorf("error must list legal values via \"supported values:\" prefix; got: %q", msg)
	}
	if !strings.Contains(msg, "claude-haiku-4-5") {
		t.Errorf("error must name the rejected model so the operator knows which vocabulary applies; got: %q", msg)
	}
	if !strings.Contains(msg, "high") {
		t.Errorf("error must echo the rejected effort value; got: %q", msg)
	}
}

// R-1GZL-PHUB: OpenAI MVP backend supports exactly gpt-5.5 with effort
// vocabulary none|low|medium|high|xhigh. "minimal" is explicitly illegal.
// An empty --effort is accepted (backend will use the pinned default per
// R-22XS-LD6T). Other 5.x models (gpt-5.5-pro, etc.) are deferred.
func TestR_1GZL_PHUB_GPT55InRegistryWithEffortVocab(t *testing.T) {
	r := Resolved{Provider: ProviderOpenAI, BareID: "gpt-5.5"}

	if err := Validate(r); err != nil {
		t.Fatalf("Validate(gpt-5.5) = %v, want nil", err)
	}

	legalEfforts := []string{"none", "low", "medium", "high", "xhigh"}
	for _, e := range legalEfforts {
		t.Run("accept_"+e, func(t *testing.T) {
			if err := ValidateEffort(r, e); err != nil {
				t.Errorf("ValidateEffort(gpt-5.5, %q) = %v, want nil", e, err)
			}
		})
	}

	// empty effort is accepted — backend applies the pinned default
	if err := ValidateEffort(r, ""); err != nil {
		t.Fatalf("ValidateEffort(gpt-5.5, \"\") = %v, want nil", err)
	}

	// "minimal" is specifically not legal on gpt-5.5
	for _, illegal := range []string{"minimal", "thinking", "xlow", "max"} {
		t.Run("reject_"+illegal, func(t *testing.T) {
			err := ValidateEffort(r, illegal)
			if err == nil {
				t.Errorf("ValidateEffort(gpt-5.5, %q) = nil, want rejection", illegal)
			}
		})
	}

	// deferred models must remain absent from the registry
	deferred := []string{"gpt-5.5-pro", "gpt-5.5-mini", "gpt-4o"}
	models := registry[ProviderOpenAI]
	for _, id := range deferred {
		if _, ok := models[id]; ok {
			t.Errorf("deferred model %q must not be in the registry", id)
		}
	}
}

// R-22XS-LD6T: when --effort is omitted on gpt-5.5, the registry-pinned
// default is "medium". DefaultEffort must return "medium" for gpt-5.5
// and "" for Anthropic models (which have no registry-level default).
func TestR_22XS_LD6T_DefaultEffortPinnedForGPT55(t *testing.T) {
	r := Resolved{Provider: ProviderOpenAI, BareID: "gpt-5.5"}
	if got := DefaultEffort(r); got != "medium" {
		t.Errorf("DefaultEffort(gpt-5.5) = %q, want %q", got, "medium")
	}

	// Anthropic models have no registry-level default
	for _, id := range []string{"claude-haiku-4-5", "claude-sonnet-4-6"} {
		ra := Resolved{Provider: ProviderAnthropic, BareID: id}
		if got := DefaultEffort(ra); got != "" {
			t.Errorf("DefaultEffort(%s) = %q, want \"\"", id, got)
		}
	}
}

// R-ZZLK-I9CK: every entry in the model registry must declare per-model
// pricing data (InputPerM, OutputPerM, and any cache rates). A model with
// unknown pricing cannot ship — silently-wrong cost totals. This test
// verifies that every current registry entry has non-zero InputPerM and
// OutputPerM, and that ModelPricing returns the same data.
func TestR_ZZLK_I9CK_RegistryHasPricingForAllModels(t *testing.T) {
	for provider, models := range registry {
		for id, spec := range models {
			t.Run(string(provider)+"/"+id, func(t *testing.T) {
				if spec.pricing.InputPerM <= 0 {
					t.Errorf("model %q has InputPerM=%v; must be > 0", id, spec.pricing.InputPerM)
				}
				if spec.pricing.OutputPerM <= 0 {
					t.Errorf("model %q has OutputPerM=%v; must be > 0", id, spec.pricing.OutputPerM)
				}
				// ModelPricing must return the same spec as the registry.
				r := Resolved{Provider: provider, BareID: id}
				got := ModelPricing(r)
				if got != spec.pricing {
					t.Errorf("ModelPricing(%q) = %+v, want %+v", id, got, spec.pricing)
				}
			})
		}
	}
}

// R-ZZLK-I9CK: ComputeCost uses per-million rates correctly.
func TestR_ZZLK_I9CK_ComputeCostUsesPerMillionRates(t *testing.T) {
	p := PricingSpec{
		InputPerM:         1.00,
		OutputPerM:        2.00,
		CacheReadPerM:     0.10,
		CacheCreationPerM: 0.20,
	}
	// 1M input + 500K output + 200K cache-read + 100K cache-creation
	got := p.ComputeCost(1_000_000, 500_000, 200_000, 100_000)
	want := 1.00 + 1.00 + 0.02 + 0.02 // = 2.04
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("ComputeCost = %v, want %v", got, want)
	}
}

// R-L4ES-AFDE: Google backend in MVP supports exactly one model:
// gemini-3.1-pro-preview (alias "pro"). Legal --effort values are
// low|medium|high; none/minimal are rejected at startup per R-ZX67-O1L1.
func TestR_L4ES_AFDE_GoogleModelInRegistryWithEffortVocab(t *testing.T) {
	r := Resolved{Provider: ProviderGoogle, BareID: "gemini-3.1-pro-preview"}

	if err := Validate(r); err != nil {
		t.Fatalf("Validate(gemini-3.1-pro-preview) = %v, want nil", err)
	}

	for _, effort := range []string{"low", "medium", "high"} {
		t.Run("accept_"+effort, func(t *testing.T) {
			if err := ValidateEffort(r, effort); err != nil {
				t.Errorf("ValidateEffort(gemini-3.1-pro-preview, %q) = %v, want nil", effort, err)
			}
		})
	}

	// empty effort accepted (backend applies pinned default per R-M1C2-M8E5)
	if err := ValidateEffort(r, ""); err != nil {
		t.Fatalf("ValidateEffort(gemini-3.1-pro-preview, \"\") = %v, want nil", err)
	}

	// none/minimal disable thinking; rejected because the model cannot disable it
	for _, illegal := range []string{"none", "minimal", "xhigh", "max"} {
		t.Run("reject_"+illegal, func(t *testing.T) {
			err := ValidateEffort(r, illegal)
			if err == nil {
				t.Errorf("ValidateEffort(gemini-3.1-pro-preview, %q) = nil, want rejection", illegal)
			}
		})
	}

	// alias "pro" must resolve to gemini-3.1-pro-preview
	got, err := Resolve("pro")
	if err != nil {
		t.Fatalf("Resolve(\"pro\") = %v, want nil error", err)
	}
	if got.BareID != "gemini-3.1-pro-preview" || got.Provider != ProviderGoogle {
		t.Errorf("Resolve(\"pro\") = %+v, want {ProviderGoogle, gemini-3.1-pro-preview}", got)
	}
}

// R-V2X8-QZDK: gemini-3.1-pro-preview uses tiered pricing with a 200K
// input-token threshold. Requests at or below the threshold bill at the
// base rates; requests above the threshold bill the entire request at
// the above-threshold rates. Both tiers and the boundary are verified.
func TestR_V2X8_QZDK_TieredPricingAppliesCorrectRates(t *testing.T) {
	r := Resolved{Provider: ProviderGoogle, BareID: "gemini-3.1-pro-preview"}
	p := ModelPricing(r)

	if p.InputTokenThreshold != 200_000 {
		t.Fatalf("InputTokenThreshold = %d, want 200000", p.InputTokenThreshold)
	}

	// ≤200K input → base rates apply
	// 100K input @ $2.00/M + 10K output @ $12.00/M = $0.20 + $0.12 = $0.32
	got := p.ComputeCost(100_000, 10_000, 0, 0)
	want := 0.32
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("≤200K: ComputeCost = %v, want %v", got, want)
	}

	// exactly 200K input → still base rates (threshold is exclusive)
	// 200K input @ $2.00/M + 10K output @ $12.00/M = $0.40 + $0.12 = $0.52
	got = p.ComputeCost(200_000, 10_000, 0, 0)
	want = 0.52
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("=200K: ComputeCost = %v, want %v", got, want)
	}

	// >200K input → above-threshold rates apply to entire request
	// 300K input @ $4.00/M + 20K output @ $18.00/M + 50K cache-read @ $0.40/M
	// = $1.20 + $0.36 + $0.02 = $1.58
	got = p.ComputeCost(300_000, 20_000, 50_000, 0)
	want = 1.58
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf(">200K: ComputeCost = %v, want %v", got, want)
	}
}

// R-Y23Q-MNSU: provider is inferred from the bare API ID's prefix —
// claude-* → Anthropic, gpt-* → OpenAI, gemini-* → Google. Pin the
// three rules with values that aren't (and won't become) registry
// entries, so this test guards the prefix mapping itself.
func TestR_Y23Q_MNSU_ProviderInferredFromPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want Provider
	}{
		{"claude-future-x", ProviderAnthropic},
		{"gpt-future-x", ProviderOpenAI},
		{"gemini-future-x", ProviderGoogle},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(tc.in)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.in, err)
			}
			if got.Provider != tc.want {
				t.Errorf("Resolve(%q).Provider = %q, want %q", tc.in, got.Provider, tc.want)
			}
		})
	}
}
